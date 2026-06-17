// Package config 负责解析与写回项目/全局的版本声明文件 .tool-versions。
// 该格式与 asdf 兼容：每行 "<tool> <version>"，# 起始为注释。
package config

import (
	"bufio"
	"os"
	"strings"
)

// ToolVersions 表示一个 .tool-versions 文件的内容（保持工具出现顺序）。
type ToolVersions struct {
	order []string
	specs map[string]string
}

// New 创建空的 ToolVersions。
func New() *ToolVersions {
	return &ToolVersions{specs: map[string]string{}}
}

// Load 读取 .tool-versions 文件；文件不存在时返回错误。
func Load(path string) (*ToolVersions, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	tv := New()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		tv.Set(fields[0], fields[1])
	}
	return tv, sc.Err()
}

// LoadOrNew 读取文件，不存在则返回空对象。
func LoadOrNew(path string) *ToolVersions {
	if tv, err := Load(path); err == nil {
		return tv
	}
	return New()
}

// Get 返回某工具的版本要求。
func (tv *ToolVersions) Get(tool string) (string, bool) {
	v, ok := tv.specs[tool]
	return v, ok
}

// Set 设置/更新某工具的版本要求。
func (tv *ToolVersions) Set(tool, version string) {
	if _, exists := tv.specs[tool]; !exists {
		tv.order = append(tv.order, tool)
	}
	tv.specs[tool] = version
}

// Tools 返回所有工具名（按文件顺序）。
func (tv *ToolVersions) Tools() []string {
	return append([]string(nil), tv.order...)
}

// Save 把内容写回指定路径。
func (tv *ToolVersions) Save(path string) error {
	var b strings.Builder
	for _, tool := range tv.order {
		b.WriteString(tool)
		b.WriteByte(' ')
		b.WriteString(tv.specs[tool])
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
