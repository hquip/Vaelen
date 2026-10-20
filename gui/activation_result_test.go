package main

import (
	"encoding/json"
	"strings"
	"testing"

	"vsm/internal/core"
)

func TestActivationResultRetainsFailedToolDiagnostics(t *testing.T) {
	check := core.CmdActivationCheck{Tools: []core.CmdToolActivation{
		{Tool: "java", Version: "17", Command: `C:\work\java.exe`, Executable: `C:\vsm\installs\java\17\bin\java.exe`, Code: "current-directory-override", Error: "当前目录同名程序", Action: "切换项目目录后重试"},
		{Tool: "node", Passed: true},
	}}
	result := activationResult("exists", "用户启动配置", check, []string{"PowerShell 配置警告"})
	if result.Verified || result.Check == nil || result.Check.Tools[0].Command != check.Tools[0].Command || !strings.Contains(result.Message, "尚未切换成功") || !strings.Contains(result.Message, "PowerShell 配置警告") {
		t.Fatalf("partial failure misreported or diagnostics lost: %+v", result)
	}
	// The Wails return value must carry the failure, rather than losing it to
	// an error response that leaves the frontend with only a transient toast.
	data, err := json.Marshal(result)
	if err != nil || !strings.Contains(string(data), `"verified":false`) || !strings.Contains(string(data), `"code":"current-directory-override"`) || !strings.Contains(string(data), `"action":`) {
		t.Fatalf("failure details missing from frontend response: %s %v", data, err)
	}
}

func TestActivationResultReportsVerifiedRepairAndEmptySelection(t *testing.T) {
	for _, tc := range []struct {
		check core.CmdActivationCheck
		want  string
	}{
		{core.CmdActivationCheck{Passed: true}, "选择工具版本后即可验证"},
		{core.CmdActivationCheck{Passed: true, PathExtRepaired: true, Tools: []core.CmdToolActivation{{Tool: "java", Passed: true}}}, "已自动修复 Windows 命令扩展名设置"},
	} {
		result := activationResult("added", "用户启动配置", tc.check, nil)
		if !result.Verified || result.Check == nil || !strings.Contains(result.Message, tc.want) {
			t.Fatalf("incorrect verification state: %+v", result)
		}
	}
}
