package config

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestSavePreservesCommentsAndBlankLines 是本次修复的核心回归测试：
// `vsm use` 改写某一门语言时，不得动到文件里的注释、空行与其它行。
func TestSavePreservesCommentsAndBlankLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".tool-versions")
	orig := "# 团队约定：勿改\n" +
		"go 1.22.0\n" +
		"\n" +
		"node 20.11.1\n" +
		"# 需要 JDK 17 编译\n" +
		"java 17\n"
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}

	tv, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	tv.Set("go", "1.23.0")
	if err := tv.Save(path); err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(path)
	want := strings.Replace(orig, "go 1.22.0", "go 1.23.0", 1)
	if string(got) != want {
		t.Fatalf("注释/空行未保留\n--- 期望 ---\n%s\n--- 实际 ---\n%s", want, got)
	}
}

// TestBOMIsStrippedAndNotDuplicated 覆盖 PowerShell/记事本写入 BOM 的场景：
// 带 BOM 的文件里改 go 不应产生第二行 go。
func TestBOMIsStrippedAndNotDuplicated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".tool-versions")
	content := append(append([]byte{}, BOM...), []byte("go 1.22.0\nnode 20.11.1\n")...)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	tv, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := tv.Get("go"); !ok || v != "1.22.0" {
		t.Fatalf("BOM 未被剥离，go 解析为 %q ok=%v", v, ok)
	}
	tv.Set("go", "1.23.0")
	if err := tv.Save(path); err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(path)
	// 断言 go 只出现一次（BOM 在文件最前面，所以用不带前导换行的计数）
	if n := strings.Count(string(got), "go "); n != 1 {
		t.Fatalf("go 行数应为 1，实际 %d：\n%s", n, got)
	}
	if !strings.Contains(string(got), "go 1.23.0") {
		t.Fatalf("新版本未写入：\n%s", got)
	}
	if !bytes.HasPrefix(got, BOM) {
		t.Fatal("原文件的 BOM 应被保留（避免每次写入都改变文件字节）")
	}
}

// TestAddNewToolAppendsWithoutClobbering 新增一门语言时不应破坏既有内容。
func TestAddNewToolAppendsWithoutClobbering(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".tool-versions")
	if err := os.WriteFile(path, []byte("# 头注释\ngo 1.22.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tv, err := LoadOrNew(path)
	if err != nil {
		t.Fatal(err)
	}
	tv.Set("python", "3.12.4")
	if err := tv.Save(path); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	want := "# 头注释\ngo 1.22.0\npython 3.12.4\n"
	if string(got) != want {
		t.Fatalf("期望:\n%q\n实际:\n%q", want, string(got))
	}
}

// TestMalformedLineIsReportedNotSilentlyDropped 只有工具名没有版本的行应被提示。
func TestMalformedLineIsReportedNotSilentlyDropped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".tool-versions")
	if err := os.WriteFile(path, []byte("go\nnode 20.11.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tv, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	w := tv.Warnings()
	if len(w) != 1 || w[0] != "go" {
		t.Fatalf("应报告 1 条异常行，实际 %v", w)
	}
}

// TestRemoveTearsDownOnlyThatTool 删除某工具只影响该行。
func TestRemoveTearsDownOnlyThatTool(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".tool-versions")
	if err := os.WriteFile(path, []byte("# c\ngo 1.22.0\nnode 20.11.1\njava 17\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tv, err := LoadOrNew(path)
	if err != nil {
		t.Fatal(err)
	}
	if !tv.Remove("node") {
		t.Fatal("应删除成功")
	}
	if err := tv.Save(path); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	want := "# c\ngo 1.22.0\njava 17\n"
	if string(got) != want {
		t.Fatalf("期望:\n%q\n实际:\n%q", want, string(got))
	}
	// 删除后 index 重建：仍能正确读取剩余条目
	tv2, _ := Load(path)
	if v, ok := tv2.Get("java"); !ok || v != "17" {
		t.Fatalf("删除后 java 应仍可解析，实际 %q ok=%v", v, ok)
	}
}

// TestSaveIsAtomicLeavesNoTempFiles 临时文件不应留在目录里。
func TestSaveIsAtomicLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".tool-versions")
	tv := New()
	tv.Set("go", "1.22.0")
	if err := tv.Save(path); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != ".tool-versions" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("目录里应只有 .tool-versions，实际 %v", names)
	}
}

// TestSavePreservesFileMode 原子替换不应把 0600 的配置改成 0644。
// 注意：Windows 不使用 POSIX 权限位，Go 会把文件一律报告为 0666，
// 因此只在类 Unix 平台断言（该断言用于守护 Linux/macOS 上的回归）。
func TestSavePreservesFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 不使用 POSIX 权限位，Go 统一报告 0666")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, ".tool-versions")
	if err := os.WriteFile(path, []byte("go 1.22.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tv, err := LoadOrNew(path)
	if err != nil {
		t.Fatal(err)
	}
	tv.Set("go", "1.23.0")
	if err := tv.Save(path); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("权限应保持 0600，实际 %v", fi.Mode().Perm())
	}
}

// TestOrderAndDupKeys 顺序稳定；重复键以最后一条为准。
func TestOrderAndDupKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".tool-versions")
	if err := os.WriteFile(path, []byte("go 1.21.0\nnode 20.11.1\ngo 1.22.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tv, _ := Load(path)
	if tools := tv.Tools(); len(tools) != 2 || tools[0] != "go" || tools[1] != "node" {
		t.Fatalf("顺序应为 [go node]，实际 %v", tools)
	}
	if v, _ := tv.Get("go"); v != "1.22.0" {
		t.Fatalf("重复键应取最后一条 1.22.0，实际 %q", v)
	}
}

// 重复声明时，设置必须改到真正生效的那一行；取消设置不能让旧声明重新生效。
func TestDuplicateToolCanBeUpdatedAndRemoved(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".tool-versions")
	before := "# keep\ngo 1.21.0\nnode 20.11.1\ngo 1.22.0\n"
	if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UnsetIn(path, "node"); err != nil { // 同时覆盖删除其它行后的索引重建
		t.Fatal(err)
	}
	if err := SetIn(path, "go", "1.23.0"); err != nil {
		t.Fatal(err)
	}
	tv, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := tv.Get("go"); got != "1.23.0" {
		t.Fatalf("设置后重读仍得到旧版本 %q", got)
	}
	if err := UnsetIn(path, "go"); err != nil {
		t.Fatal(err)
	}
	tv, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := tv.Get("go"); exists {
		t.Fatal("取消设置后旧声明重新生效")
	}
	if tv.Text() != "# keep\n" {
		t.Fatalf("应只保留注释，得到 %q", tv.Text())
	}
}

func TestSetInPreservesFileOnReadFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".tool-versions")
	// 超过解析器行长限制，稳定复现读入失败，避免依赖各平台权限模型。
	before := "#" + strings.Repeat("x", 1024*1024) + "\ngo 1.22.0\n"
	if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetIn(path, "go", "1.23.0"); err == nil {
		t.Fatal("原文件读入失败时不能当作新文件覆盖")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != before {
		t.Fatalf("读入失败后原文件被修改: %v", err)
	}
}

func TestVersionConfigCannotReadExternalSymlink(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "private")
	if err := os.WriteFile(outside, []byte("sensitive-value"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), ".tool-versions")
	if err := os.Symlink(outside, path); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("file symlink creation requires Windows developer mode")
		}
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("configuration followed an external symlink")
	}
	if err := SetIn(path, "go", "1.23.0"); err == nil {
		t.Fatal("unsafe configuration link was accepted for writing")
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "sensitive-value" {
		t.Fatal("external file was changed")
	}
}

// TestSetInUnsetIn 便捷函数行为。
func TestSetInUnsetIn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".tool-versions")
	if err := SetIn(path, "go", "1.22.0"); err != nil {
		t.Fatal(err)
	}
	if err := SetIn(path, "node", "20"); err != nil {
		t.Fatal(err)
	}
	if err := UnsetIn(path, "go"); err != nil {
		t.Fatal(err)
	}
	tv, _ := Load(path)
	if _, ok := tv.Get("go"); ok {
		t.Fatal("go 应已删除")
	}
	if v, ok := tv.Get("node"); !ok || v != "20" {
		t.Fatalf("node 应保留，实际 %q ok=%v", v, ok)
	}
	// 对不存在的文件执行 UnsetIn 不应报错
	if err := UnsetIn(filepath.Join(dir, "nope"), "go"); err != nil {
		t.Fatalf("删除不存在的文件不应报错: %v", err)
	}
}
