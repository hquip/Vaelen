package plugin

import "testing"

func TestCompareVer(t *testing.T) {
	if compareVer("1.22.0", "1.21.0") <= 0 {
		t.Error("1.22.0 应大于 1.21.0")
	}
	if compareVer("1.0", "1.0.0") != 0 {
		t.Error("1.0 应等于 1.0.0")
	}
	if compareVer("2.0", "10.0") >= 0 {
		t.Error("2.0 应小于 10.0")
	}
}

func TestDetectCommandsAliases(t *testing.T) {
	cmds := DetectCommands()
	// 内置插件至少应包含 go / node。
	if _, ok := cmds["go"]; !ok {
		t.Error("缺少 go 命令映射")
	}
	if py, ok := cmds["python"]; ok {
		has3 := false
		for _, n := range py {
			if n == "python3" {
				has3 = true
			}
		}
		if !has3 {
			t.Errorf("python 候选命令应含 python3，实际 %v", py)
		}
	}
}
