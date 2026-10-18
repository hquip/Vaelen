package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"vsm/internal/config"
	"vsm/internal/mcpserver"
	"vsm/internal/plugin"
)

type ImportPreview struct {
	Source      string            `json:"source"`
	Tools       map[string]string `json:"tools"`
	Unsupported []string          `json:"unsupported"`
	Applied     bool              `json:"applied"`
	Notice      string            `json:"notice"`
}

// ImportMise reads only resolved tool versions. Safe mode prevents loading
// executable project environment directives; tasks/secrets are never imported.
func ImportMise(dir, file string, apply bool) (ImportPreview, error) {
	out := ImportPreview{Source: "mise", Tools: map[string]string{}, Unsupported: []string{}, Notice: "仅导入工具版本；不复制任务、秘密变量或安装脚本。"}
	var data []byte
	var err error
	if file != "" {
		data, err = config.ReadBounded(file, 4<<20)
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "mise", "ls", "--current", "--json")
		cmd.Dir = dir
		cmd.Env = replaceEnv(os.Environ(), "MISE_SAFE", "1")
		mcpserver.ConfigureProcess(cmd)
		cmd.WaitDelay = 2 * time.Second
		var limited taskOutput
		cmd.Stdout = &limited
		cmd.Stderr = io.Discard
		err = cmd.Run()
		data = limited.buf.Bytes()
		if limited.truncated {
			return out, fmt.Errorf("mise 输出过大，请从导出的 JSON 文件导入")
		}
	}
	if err != nil {
		return out, fmt.Errorf("读取 mise 工具失败（可用 --file 导出的 JSON）: %w", err)
	}
	var tools map[string][]struct {
		Version   string `json:"version"`
		Requested string `json:"requested_version"`
	}
	if err := json.Unmarshal(data, &tools); err != nil {
		return out, fmt.Errorf("无效的 mise ls --current --json 输出: %w", err)
	}
	for name, versions := range tools {
		if len(versions) == 0 {
			continue
		}
		normalized := name
		switch normalized {
		case "nodejs":
			normalized = "node"
		case "golang":
			normalized = "go"
		}
		if _, ok := plugin.Get(normalized); !ok {
			out.Unsupported = append(out.Unsupported, name)
			continue
		}
		if len(versions) != 1 {
			return out, fmt.Errorf("%s 存在多个当前版本，请先明确选择", name)
		}
		version := versions[0].Version
		if err := validateInstall(normalized, version); err != nil {
			return out, err
		}
		out.Tools[normalized] = version
	}
	if !apply {
		return out, nil
	}
	if len(out.Unsupported) > 0 {
		return out, fmt.Errorf("尚未登记工具: %s；先用 vsm tools add 添加来源", strings.Join(out.Unsupported, ", "))
	}
	if len(out.Tools) == 0 {
		return out, fmt.Errorf("没有可导入的工具")
	}
	root, err := ProjectDirectory(dir)
	if err != nil {
		return out, err
	}
	path := filepath.Join(root, config.WorkspaceName)
	if _, err := os.Lstat(path); err == nil {
		return out, fmt.Errorf("vsm.json 已存在，不覆盖；先检查导入预览")
	} else if !os.IsNotExist(err) {
		return out, err
	}
	c := config.Workspace{Schema: 1, Tools: out.Tools}
	data, err = json.MarshalIndent(c, "", "  ")
	if err != nil {
		return out, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return out, err
	}
	_, err = f.Write(append(data, '\n'))
	closeErr := f.Close()
	if err != nil {
		return out, err
	}
	if closeErr != nil {
		return out, closeErr
	}
	out.Applied = true
	return out, nil
}
