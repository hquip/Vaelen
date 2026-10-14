package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeExitErr 模拟带退出码的错误（与 core.exitCodeError 同一约定）。
type fakeExitErr struct{ code int }

func (e fakeExitErr) Error() string { return "exit" }
func (e fakeExitErr) ExitCode() int { return e.code }

// TestExitCoderIsDetected 退出码透传依赖 errors.As 能穿透包装找到 exitCoder。
func TestExitCoderIsDetected(t *testing.T) {
	wrapped := errors.Join(errors.New("ctx"), fakeExitErr{code: 42})
	var ec exitCoder
	if !errors.As(wrapped, &ec) || ec.ExitCode() != 42 {
		t.Fatalf("应能从包装错误中取出退出码 42")
	}
}

// buildVsm 把当前代码编译成临时二进制，用于端到端验证真实进程行为。
func buildVsm(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "vsm")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, "vsm")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("编译 vsm 失败: %v\n%s", err, out)
	}
	return bin
}

// runVsm 在隔离的 VSM_HOME 与工作目录里运行 vsm，返回退出码与输出。
func runVsm(t *testing.T, bin, home, cwd string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "VSM_HOME="+home)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else {
			t.Fatalf("运行失败: %v", err)
		}
	}
	return code, string(out)
}

// TestCLIEndToEnd 端到端覆盖本次修复的 CLI 行为（真实进程、真实退出码）。
func TestCLIEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("短模式跳过端到端测试")
	}
	bin := buildVsm(t)
	home := t.TempDir()
	cwd := t.TempDir()

	t.Run("未知命令返回非零", func(t *testing.T) {
		if code, _ := runVsm(t, bin, home, cwd, "bogus-cmd"); code == 0 {
			t.Fatal("未知命令应返回非零退出码")
		}
	})

	t.Run("use 拒绝多余参数", func(t *testing.T) {
		code, out := runVsm(t, bin, home, cwd, "use", "go", "1.22.0", "1.23.0")
		if code == 0 {
			t.Fatalf("多余参数应报错，实际成功：%s", out)
		}
		if _, err := os.Stat(filepath.Join(cwd, ".tool-versions")); err == nil {
			t.Fatal("参数错误时不应写出 .tool-versions")
		}
	})

	t.Run("use 拒绝空版本", func(t *testing.T) {
		if code, out := runVsm(t, bin, home, cwd, "use", "go", ""); code == 0 {
			t.Fatalf("空版本应报错：%s", out)
		}
	})

	t.Run("use 保留注释与空行", func(t *testing.T) {
		dir := t.TempDir()
		orig := "# 团队约定：勿改\ngo 1.22.0\n\nnode 20.11.1\n"
		if err := os.WriteFile(filepath.Join(dir, ".tool-versions"), []byte(orig), 0o644); err != nil {
			t.Fatal(err)
		}
		if code, out := runVsm(t, bin, home, dir, "use", "go", "1.23.0"); code != 0 {
			t.Fatalf("use 应成功：%s", out)
		}
		got, _ := os.ReadFile(filepath.Join(dir, ".tool-versions"))
		want := strings.Replace(orig, "go 1.22.0", "go 1.23.0", 1)
		if string(got) != want {
			t.Fatalf("注释/空行被破坏\n期望:\n%q\n实际:\n%q", want, got)
		}
	})

	t.Run("install 结尾裸 --path 给出清晰报错", func(t *testing.T) {
		code, out := runVsm(t, bin, home, cwd, "install", "go", "--path")
		if code == 0 || !strings.Contains(out, "--path 需要指定目录") {
			t.Fatalf("应提示 --path 需要目录，实际 code=%d：%s", code, out)
		}
	})

	t.Run("env 未知 shell 在空目录也报错", func(t *testing.T) {
		if code, _ := runVsm(t, bin, home, t.TempDir(), "env", "nosuchshell"); code == 0 {
			t.Fatal("未知 shell 应在任何 cwd 下都报错")
		}
	})

	t.Run("env 无工具时仍重建 PATH", func(t *testing.T) {
		code, out := runVsm(t, bin, home, t.TempDir(), "env", "bash")
		if code != 0 || !strings.Contains(out, "VSM_PATH_BASE") {
			t.Fatalf("离开项目时必须输出 PATH 重建语句，实际 code=%d：%q", code, out)
		}
	})

	t.Run("install-root 存绝对路径", func(t *testing.T) {
		if code, out := runVsm(t, bin, home, cwd, "install-root", "rel-dir"); code != 0 {
			t.Fatalf("install-root 应成功：%s", out)
		}
		raw, err := os.ReadFile(filepath.Join(home, "install-root"))
		if err != nil {
			t.Fatal(err)
		}
		if p := strings.TrimSpace(string(raw)); !filepath.IsAbs(p) {
			t.Fatalf("install-root 应保存绝对路径，实际 %q", p)
		}
		if code, out := runVsm(t, bin, home, cwd, "install-root", "default"); code != 0 {
			t.Fatalf("恢复默认应成功：%s", out)
		}
	})

	t.Run("doctor 可运行并输出 JSON", func(t *testing.T) {
		code, out := runVsm(t, bin, home, cwd, "doctor", "--json")
		if code != 0 || !strings.Contains(out, `"issues"`) {
			t.Fatalf("doctor --json 应输出 issues，实际 code=%d：%s", code, out)
		}
	})

	t.Run("completion 生成脚本", func(t *testing.T) {
		for _, sh := range []string{"bash", "zsh", "fish", "powershell"} {
			code, out := runVsm(t, bin, home, cwd, "completion", sh)
			if code != 0 || !strings.Contains(out, "vsm") {
				t.Fatalf("%s 补全生成失败 code=%d", sh, code)
			}
		}
	})

	t.Run("list --json 输出合法结构", func(t *testing.T) {
		code, out := runVsm(t, bin, home, cwd, "list", "go", "--json")
		if code != 0 || !strings.Contains(out, `"tool": "go"`) {
			t.Fatalf("list --json 输出异常 code=%d：%s", code, out)
		}
	})
}
