package config

import (
	"path/filepath"
	"testing"
)

func TestToolVersionsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".tool-versions")
	tv := New()
	tv.Set("go", "1.22.0")
	tv.Set("node", "20.11.1")
	tv.Set("go", "1.21.5") // 更新已存在的工具，顺序应保持

	if err := tv.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := loaded.Get("go"); v != "1.21.5" {
		t.Errorf("go=%q want 1.21.5", v)
	}
	if v, _ := loaded.Get("node"); v != "20.11.1" {
		t.Errorf("node=%q want 20.11.1", v)
	}
	if tools := loaded.Tools(); len(tools) != 2 || tools[0] != "go" || tools[1] != "node" {
		t.Errorf("顺序=%v want [go node]", tools)
	}
	if _, ok := loaded.Get("python"); ok {
		t.Error("python 不应存在")
	}
}

func TestLoadMissing(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("加载不存在的文件应返回错误")
	}
	if tv := LoadOrNew(filepath.Join(t.TempDir(), "nope")); tv == nil {
		t.Error("LoadOrNew 不应返回 nil")
	}
}
