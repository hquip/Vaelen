package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Only fixed version flags enter cmd.exe, never user command arguments. /d and
// /v:off disable AutoRun and delayed expansion; reject percent expansion paths.
func configureVersionProbe(c *exec.Cmd, bin, flag string) error {
	ext := strings.ToLower(filepath.Ext(bin))
	if ext != ".cmd" && ext != ".bat" {
		return nil
	}
	if strings.ContainsAny(bin, "%\"\r\n\x00") {
		return fmt.Errorf("批处理版本探测不支持该路径，请选择安装目录以读取元数据")
	}
	shell := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	c.Path = shell
	c.Args = []string{shell, "/d", "/v:off", "/s", "/c", `"` + bin + `" ` + flag}
	c.SysProcAttr.CmdLine = `"` + shell + `" /d /v:off /s /c ""` + bin + `" ` + flag + `"`
	return nil
}
