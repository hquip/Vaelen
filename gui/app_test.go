package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	t.Setenv("VSM_HOME", t.TempDir())
	return NewApp()
}

// TestSetLocalRequiresProjectDir macOS 从 Finder 启动时工作目录是 "/"，
// 「设为本目录」不能往那里写，而应要求用户先选择项目目录。
func TestSetLocalRequiresProjectDir(t *testing.T) {
	a := newTestApp(t)
	if err := a.SetProjectDir(""); err != nil {
		t.Fatal(err)
	}
	if err := a.SetLocal("go", "1.22.0"); !errors.Is(err, errNoProjectDir) {
		t.Fatalf("未选择项目目录时应返回 errNoProjectDir，实际 %v", err)
	}

	proj := t.TempDir()
	if err := a.SetProjectDir(proj); err != nil {
		t.Fatal(err)
	}
	if err := a.SetLocal("go", "1.22.0"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(proj, ".tool-versions"))
	if err != nil || !strings.Contains(string(got), "go 1.22.0") {
		t.Fatalf("应写入项目目录的 .tool-versions，实际 %q err=%v", got, err)
	}
	if a.CurrentDir() != proj {
		t.Fatalf("CurrentDir 应返回项目目录，实际 %q", a.CurrentDir())
	}
}

func TestSetProjectDirValidates(t *testing.T) {
	a := newTestApp(t)
	f := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.SetProjectDir(f); err == nil {
		t.Fatal("文件不能作为项目目录")
	}
	if err := a.SetProjectDir(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("不存在的目录应报错")
	}
}

// TestBusyOperationsFailFast 有写操作进行中时，其它写操作应立即返回「正在忙」，
// 而不是在后台排队数分钟（前端按钮会显得毫无反应）。
func TestBusyOperationsFailFast(t *testing.T) {
	a := newTestApp(t)
	done, err := a.begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Uninstall("go", "1.22.0"); !errors.Is(err, errBusy) {
		t.Fatalf("应返回 errBusy，实际 %v", err)
	}
	if err := a.SetInstallRoot(t.TempDir()); !errors.Is(err, errBusy) {
		t.Fatalf("应返回 errBusy，实际 %v", err)
	}
	if err := a.SetGlobal("java", "21"); !errors.Is(err, errBusy) {
		t.Fatalf("global switch must share write lock: %v", err)
	}
	done()
	if _, err := a.begin(); err != nil {
		t.Fatalf("释放后应可再次获取: %v", err)
	}
}

func TestSetGlobalRequiresMatchingCLIAndRefreshesShim(t *testing.T) {
	a := newTestApp(t)
	if err := a.SetProjectDir(""); err != nil {
		t.Fatal(err)
	}
	c := *a.c()
	c.act.VsmExe = ""
	a.st.Store(&c)
	if err := a.SetGlobal("java", "21"); err == nil || !strings.Contains(err.Error(), "CLI") {
		t.Fatalf("missing CLI accepted: %v", err)
	}
	if _, err := os.Stat(c.paths.GlobalToolVersions()); !os.IsNotExist(err) {
		t.Fatalf("failed switch wrote config: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "vsm")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	// A real paired CLI establishes the version protocol used by both activation operations.
	cmd := exec.Command("go", "build", "-o", bin, "vsm")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v %s", err, out)
	}
	c.act.VsmExe = bin
	if err := checkActivationCLI(bin); err != nil {
		t.Fatal(err)
	}
	toolDir := filepath.Join(c.paths.InstallDir("java", "21"), "bin")
	if err := os.MkdirAll(toolDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := c.paths.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	name := "java"
	if runtime.GOOS == "windows" {
		name += ".cmd"
	}
	shim := filepath.Join(c.paths.Shims(), name)
	if err := os.WriteFile(shim, []byte("old-vsm exec java"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := a.SetGlobal("java", "21"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(shim)
	if strings.Contains(string(data), "old-vsm") || !strings.Contains(string(data), bin) {
		t.Fatalf("stale shim: %s", data)
	}
	// The same helper must refuse a previous build before changing activation state.
	old := filepath.Join(t.TempDir(), filepath.Base(bin))
	cmd = exec.Command("go", "build", "-ldflags", "-X vsm/internal/buildinfo.Version=0.1.0", "-o", old, "vsm")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build legacy CLI: %v %s", err, out)
	}
	c.act.VsmExe = old
	if err := a.SetGlobal("java", "17"); err == nil || !strings.Contains(err.Error(), "版本不一致") {
		t.Fatalf("legacy CLI accepted: %v", err)
	}
	data, _ = os.ReadFile(c.paths.GlobalToolVersions())
	if !strings.Contains(string(data), "java 21") {
		t.Fatalf("failed switch changed default: %s", data)
	}
}

// TestConcurrentStatusAndInstallRootSwitch 在 -race 下运行：切换安装根目录时，
// 其它 goroutine 并发读取状态不应产生数据竞争（组件以原子快照整体替换）。
func TestConcurrentStatusAndInstallRootSwitch(t *testing.T) {
	a := newTestApp(t)
	roots := []string{t.TempDir(), t.TempDir()}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := a.Status(); err != nil {
					t.Error(err)
					return
				}
				_ = a.GetInstallRoot()
				_ = a.DataDir()
			}
		}()
	}
	for i := 0; i < 20; i++ {
		if err := a.SetInstallRoot(roots[i%2]); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
	if got := a.GetInstallRoot(); got != roots[1] {
		t.Fatalf("最终安装根目录应为 %s，实际 %s", roots[1], got)
	}
}

// TestSetInstallRootStoresAbsolutePath 相对路径要转成绝对路径再保存。
func TestSetInstallRootStoresAbsolutePath(t *testing.T) {
	a := newTestApp(t)
	wd, _ := os.Getwd()
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	if err := a.SetInstallRoot("rel-root"); err != nil {
		t.Fatal(err)
	}
	if got := a.GetInstallRoot(); !filepath.IsAbs(got) {
		t.Fatalf("安装根目录应为绝对路径，实际 %q", got)
	}
	if err := a.SetInstallRoot(""); err != nil {
		t.Fatalf("恢复默认应成功: %v", err)
	}
}

func TestStatusReturnsNonNilInstalled(t *testing.T) {
	a := newTestApp(t)
	st, err := a.Status()
	if err != nil {
		t.Fatal(err)
	}
	if len(st) == 0 {
		t.Fatal("应返回全部工具状态")
	}
	for _, s := range st {
		if s.Installed == nil {
			t.Fatalf("%s 的 installed 不应为 null（前端直接取 .length）", s.Tool)
		}
	}
}
