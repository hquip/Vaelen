// Package config 解析与写回 .tool-versions（兼容 asdf：每行 "<tool> <version>"，# 起首为注释）。
package config

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ToolVersions 表示一个 .tool-versions 文件的内容。
//
// 关键设计：**保留原始行**。早期实现把文件解析成 map 后重新生成，
// 导致注释、空行、行内多余字段在写回时全部丢失（`vsm use` 会毁掉用户的
// 配置文件格式，已由测试 TestSavePreservesCommentsAndBlankLines 覆盖）。
// 现在只在命中的那一行上做替换，其余内容逐字节保留。
type ToolVersions struct {
	lines   []string          // 原始行（含空行与注释），不含换行符
	index   map[string]int    // 工具名 -> lines 下标
	specs   map[string]string // 工具名 -> 版本要求
	order   []string          // 首次出现的工具名顺序
	hadBOM  bool              // 原文件带 UTF-8 BOM（写回时保持一致）
	missing []string          // 无法解析的行（会通过 Warnings 报出）
}

// BOM 是 UTF-8 字节序标记。Windows 下 PowerShell 5.1 的 Set-Content/重定向、
// 记事本、以及 VS Code 的「UTF-8 with BOM」都会写入它；不剥离会让首个工具名
// 带上前缀，从而查不到也写不回去（静默产生重复条目）。
var BOM = []byte{0xEF, 0xBB, 0xBF}

// bomRune 是 BOM 的字符串形式，用于按行剥离（用 rune 构造，避免源码中出现该字符）。
const bomRune = string(rune(0xFEFF))

// New 创建空的 ToolVersions。
func New() *ToolVersions {
	return &ToolVersions{
		index: map[string]int{},
		specs: map[string]string{},
	}
}

// Load 读取 .tool-versions；文件不存在时返回错误（调用方用 LoadOrNew 兜底）。
func Load(path string) (*ToolVersions, error) {
	f, err := os.OpenInRoot(filepath.Dir(path), filepath.Base(path))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (4<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 4<<20 {
		return nil, fmt.Errorf("version declaration file exceeds 4 MiB limit: %s", path)
	}
	tv := New()
	if bytes.HasPrefix(raw, BOM) {
		tv.hadBOM = true
		raw = raw[len(BOM):]
	}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		tv.addLine(sc.Text())
	}
	return tv, sc.Err()
}

// LoadOrNew 只在文件不存在时创建空对象；其它读入错误必须交给调用方，避免覆盖原文件。
func LoadOrNew(path string) (*ToolVersions, error) {
	tv, err := Load(path)
	if os.IsNotExist(err) {
		return New(), nil
	}
	return tv, err
}

// Exists 判断文件是否已存在（用于区分「新建」与「改写」，保留原文件权限）。
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// addLine 记录一行原始内容，并在可解析时登记工具名与版本要求。
func (tv *ToolVersions) addLine(line string) {
	idx := len(tv.lines)
	tv.lines = append(tv.lines, line)

	trimmed := strings.TrimPrefix(strings.TrimSpace(line), bomRune)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return
	}
	fields := strings.Fields(trimmed)
	if len(fields) < 2 {
		// 只有工具名、没有版本：记录下来以便提示用户，而不是静默丢弃
		tv.missing = append(tv.missing, trimmed)
		return
	}
	tool := fields[0]
	if _, dup := tv.index[tool]; dup {
		// 重复键：保留后写入的那条（与 asdf 行为一致），另一条仍留在文件里
		tv.index[tool] = idx
		tv.specs[tool] = fields[1]
		return
	}
	tv.index[tool] = idx
	tv.specs[tool] = fields[1]
	tv.order = append(tv.order, tool)
}

// Warnings 返回解析时被跳过的行（只有工具名没有版本等），供 CLI 提示用户。
func (tv *ToolVersions) Warnings() []string {
	return append([]string(nil), tv.missing...)
}

// Get 返回某工具的版本要求。
func (tv *ToolVersions) Get(tool string) (string, bool) {
	v, ok := tv.specs[tool]
	return v, ok
}

// Set 设置/更新某工具的版本要求。命中已有行时**就地替换**，保留其余内容。
func (tv *ToolVersions) Set(tool, version string) {
	if i, exists := tv.index[tool]; exists {
		indent := tv.lines[i][:len(tv.lines[i])-len(strings.TrimLeft(tv.lines[i], " \t"))]
		tv.lines[i] = indent + tool + " " + version
	} else {
		tv.index[tool] = len(tv.lines)
		tv.lines = append(tv.lines, tool+" "+version)
		tv.order = append(tv.order, tool)
	}
	tv.specs[tool] = version
}

// Remove 删除某工具的所有声明行，避免早期重复声明在重新加载后复活。
func (tv *ToolVersions) Remove(tool string) bool {
	if _, ok := tv.index[tool]; !ok {
		return false
	}
	updated := New()
	updated.hadBOM = tv.hadBOM
	for _, line := range tv.lines {
		fields := strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), bomRune))
		if len(fields) >= 2 && fields[0] == tool {
			continue
		}
		updated.addLine(line)
	}
	*tv = *updated
	return true
}

// Tools 返回声明过的工具名（按文件中的出现顺序）。
func (tv *ToolVersions) Tools() []string {
	return append([]string(nil), tv.order...)
}

// Text 渲染最终文件内容（保留注释与空行，保持原有 BOM 状态）。
func (tv *ToolVersions) Text() string {
	var b strings.Builder
	if tv.hadBOM {
		b.Write(BOM)
	}
	for i, line := range tv.lines {
		b.WriteString(line)
		if i < len(tv.lines)-1 {
			b.WriteByte('\n')
		}
	}
	// 有内容时以换行收尾（POSIX 友好，且避免下次追加粘连到上一行）
	if len(tv.lines) > 0 && tv.lines[len(tv.lines)-1] != "" {
		b.WriteByte('\n')
	}
	return b.String()
}

// Save 原子写入 .tool-versions（见 WriteFileAtomic）。
// 早期实现直接 os.WriteFile（O_TRUNC 原地写），中断/并发时会留下被截断的配置文件。
func (tv *ToolVersions) Save(path string) error {
	return WriteFileAtomic(path, []byte(tv.Text()), 0o644)
}

// WriteFileAtomic 原子写入文件：先写同目录临时文件并 fsync，再 rename 覆盖。
// 目标已存在时沿用其权限，否则使用 perm。用于 .tool-versions 以及
// mirror / proxy / install-root 这类配置文件——中断或并发写入时，
// 读者看到的要么是旧内容、要么是新内容，不会是被截断的一半。
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	mode := perm
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // rename 成功后此处为 no-op
	// CreateTemp 固定用 0600，这里显式对齐目标权限（Windows 上也会写只读位）
	if err := os.Chmod(tmpName, mode); err != nil {
		tmp.Close()
		return err
	}

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("写入 %s 失败: %w", path, err)
	}
	return nil
}

// SetIn 是便捷函数：在 path 中把 tool 设为 version（文件不存在则创建）。
func SetIn(path, tool, version string) error {
	tv, err := LoadOrNew(path)
	if err != nil {
		return err
	}
	tv.Set(tool, version)
	return tv.Save(path)
}

// UnsetIn 是便捷函数：从 path 中删除 tool 的声明。
func UnsetIn(path, tool string) error {
	tv, err := Load(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !tv.Remove(tool) {
		return nil
	}
	return tv.Save(path)
}
