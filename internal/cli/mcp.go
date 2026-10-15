package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
	"vsm/internal/buildinfo"
	"vsm/internal/core"
	"vsm/internal/mcpconfig"
	"vsm/internal/mcpserver"
	"vsm/internal/plugin"
)

func (a *app) cmdMCP(args []string) error {
	operation := "serve"
	if len(args) > 0 && (args[0] == "serve" || args[0] == "config") {
		operation = args[0]
		args = args[1:]
	}
	project := ""
	readOnly := false
	client := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--project":
			if i+1 >= len(args) {
				return fmt.Errorf("--project needs a directory")
			}
			project = args[i+1]
			i++
		case "--read-only":
			readOnly = true
		case "--client":
			if operation != "config" {
				return fmt.Errorf("--client 仅用于 vsm mcp config")
			}
			if i+1 >= len(args) || args[i+1] == "" || strings.HasPrefix(args[i+1], "--") {
				return fmt.Errorf("--client needs a client name")
			}
			client = args[i+1]
			i++
		default:
			return fmt.Errorf("用法: vsm mcp [serve|config] --project <目录> [--read-only]；config 可加 --client cursor|claude-desktop|claude-code|codex|vscode|windsurf|generic")
		}
	}
	if project == "" {
		return fmt.Errorf("MCP requires an explicit --project directory")
	}
	root, err := core.ProjectDirectory(project)
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if operation == "config" {
		args := []string{"mcp", "serve", "--project", root}
		if readOnly {
			args = append(args, "--read-only")
		}
		config, err := mcpconfig.Generate(client, runtime.GOOS, executable, args)
		if err != nil {
			return err
		}
		_, err = fmt.Print(config.Content)
		return err
	}
	server := mcpserver.Server{Version: buildinfo.Version, Tools: mcpTools(readOnly)}
	server.Call = func(ctx context.Context, name string, raw json.RawMessage) (mcpserver.Result, error) {
		return mcpCall(ctx, executable, root, name, raw)
	}
	return server.Serve(context.Background(), os.Stdin, os.Stdout)
}

func mcpTools(readOnly bool) []mcpserver.Tool {
	field := func(description string) map[string]any {
		return map[string]any{"type": "string", "description": description}
	}
	makeTool := func(name, description string, properties map[string]any, required []string, read bool) mcpserver.Tool {
		if required == nil {
			required = []string{}
		}
		return mcpserver.Tool{Name: name, Description: description, InputSchema: map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}, Annotations: map[string]any{"readOnlyHint": read, "destructiveHint": !read, "idempotentHint": read, "openWorldHint": !read}}
	}
	tools := []mcpserver.Tool{
		makeTool("vsm_context", "Read the bound project's required, resolved and active language versions. No configuration changes.", map[string]any{}, nil, true),
		makeTool("vsm_project_status", "Read project isolation mode, paths and Docker image.", map[string]any{}, nil, true),
		makeTool("vsm_list", "List installed versions in this project's environment.", map[string]any{"tool": field("Optional registered tool name")}, nil, true),
	}
	if readOnly {
		return tools
	}
	return append(tools,
		makeTool("vsm_project_init", "Initialize one environment for the bound project. local is the default; docker needs an explicit image.", map[string]any{"mode": map[string]any{"type": "string", "enum": []string{"local", "versions", "docker"}}, "image": field("Docker image containing the required toolchains")}, nil, false),
		makeTool("vsm_use", "Set a concrete version or numeric prefix in the bound project's .tool-versions. Does not change system settings.", map[string]any{"tool": field("Registered language/tool name"), "version": field("Concrete version, numeric prefix or latest")}, []string{"tool", "version"}, false),
		makeTool("vsm_install", "Install a verified tool version in a local project environment. Publisher SHA256 is required.", map[string]any{"tool": field("Registered tool name"), "version": field("Version or latest")}, []string{"tool"}, false),
		makeTool("vsm_run", "Execute a command using this project's toolchains. This runs project code and can modify project files. Local mode is an environment boundary, not an OS sandbox.", map[string]any{"command": field("Command name, e.g. go, node or python"), "args": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "install_missing": map[string]any{"type": "boolean"}, "timeout_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": 600}}, []string{"command"}, false),
	)
}

type mcpArguments struct {
	Tool           string   `json:"tool"`
	Version        string   `json:"version"`
	Mode           string   `json:"mode"`
	Image          string   `json:"image"`
	Command        string   `json:"command"`
	Args           []string `json:"args"`
	InstallMissing bool     `json:"install_missing"`
	Timeout        int      `json:"timeout_seconds"`
}

func mcpCall(ctx context.Context, executable, root, name string, raw json.RawMessage) (mcpserver.Result, error) {
	var supplied map[string]json.RawMessage
	if err := json.Unmarshal(raw, &supplied); err != nil || supplied == nil {
		return mcpserver.Result{}, fmt.Errorf("arguments must be an object")
	}
	for _, tool := range mcpTools(false) {
		if tool.Name == name {
			properties := tool.InputSchema["properties"].(map[string]any)
			for key := range supplied {
				if _, ok := properties[key]; !ok {
					return mcpserver.Result{}, fmt.Errorf("unexpected argument %q for %s", key, name)
				}
			}
			break
		}
	}
	var params mcpArguments
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&params); err != nil {
		return mcpserver.Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return mcpserver.Result{}, fmt.Errorf("arguments must be an object")
	}
	timeout := 120
	if params.Timeout != 0 {
		timeout = params.Timeout
	}
	if timeout < 1 || timeout > 600 {
		return mcpserver.Result{}, fmt.Errorf("timeout_seconds must be 1..600")
	}
	if params.Tool != "" {
		if _, ok := plugin.Get(params.Tool); !ok {
			return mcpserver.Result{}, unsupported(params.Tool)
		}
	}
	if params.Version != "" && !regexp.MustCompile(`^(latest|[0-9][0-9A-Za-z._+-]*)$`).MatchString(params.Version) {
		return mcpserver.Result{}, fmt.Errorf("version must be a concrete version, numeric prefix or latest")
	}
	project, err := core.FindProject(root)
	if err != nil {
		return mcpserver.Result{}, err
	}
	if project != nil && !sameMCPRoot(project.Root, root) {
		project = nil
	}
	var args []string
	switch name {
	case "vsm_context":
		args = []string{"context", "--json"}
	case "vsm_project_status":
		args = []string{"project", "status", "--json"}
	case "vsm_list":
		args = []string{"list", "--json"}
		if params.Tool != "" {
			args = append(args, params.Tool)
		}
	case "vsm_project_init":
		args = []string{"project", "init"}
		if params.Mode != "" {
			args = append(args, "--mode", params.Mode)
		}
		if params.Image != "" {
			args = append(args, "--image", params.Image)
		}
	case "vsm_use":
		if params.Tool == "" || params.Version == "" {
			return mcpserver.Result{}, fmt.Errorf("tool and version are required")
		}
		args = []string{"use", "--", params.Tool, params.Version}
	case "vsm_install":
		if project == nil || project.Mode != "local" {
			return mcpserver.Result{}, fmt.Errorf("initialize --mode local before installing through MCP")
		}
		if params.Tool == "" {
			return mcpserver.Result{}, fmt.Errorf("tool is required")
		}
		if params.Version == "" {
			params.Version = "latest"
		}
		args = []string{"install", params.Tool, params.Version, "--require-checksum"}
	case "vsm_run":
		if project == nil {
			return mcpserver.Result{}, fmt.Errorf("initialize a project environment before running commands")
		}
		if params.Command == "" || strings.ContainsAny(params.Command, "/\\\x00\r\n") {
			return mcpserver.Result{}, fmt.Errorf("command must be a command name")
		}
		if params.InstallMissing && project.Mode == "versions" {
			return mcpserver.Result{}, fmt.Errorf("MCP will not install into the shared global environment; use local mode")
		}
		args = []string{"run"}
		if params.InstallMissing {
			args = append(args, "--install-missing")
		}
		args = append(args, "--", params.Command)
		args = append(args, params.Args...)
	default:
		return mcpserver.Result{}, fmt.Errorf("unknown MCP tool")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir = root
	// Subprocesses get a fixed boundary. Tool arguments cannot change cwd or enable
	// global installation, uninstall, shell activation or system PATH writes.
	cmd.Env = setEnv(os.Environ(), "VSM_PROJECT_BOUNDARY", root)
	if project != nil {
		cmd.Env = setEnv(cmd.Env, "VSM_MCP_EXPECT_MODE", project.Mode)
	}
	requestID := rand.Text()
	cmd.Env = setEnv(cmd.Env, "VSM_MCP_REQUEST_ID", requestID)
	mcpserver.ConfigureProcess(cmd)
	cmd.WaitDelay = 2 * time.Second
	stdout, stderr := &cappedOutput{limit: 1 << 20}, &cappedOutput{limit: 64 << 10}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	runErr := cmd.Run()
	if ctx.Err() != nil && project != nil && project.Mode == "docker" {
		cleanupMCPContainers(requestID)
	}
	code := 0
	if runErr != nil {
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	result := map[string]any{"project": root, "exit_code": code, "stdout": stdout.String(), "stderr": stderr.String(), "truncated": stdout.truncated || stderr.truncated}
	if runErr == nil {
		var data map[string]any
		if json.Unmarshal(stdout.buf.Bytes(), &data) == nil {
			result["data"] = data
		}
	}
	if ctx.Err() != nil {
		result["error"] = ctx.Err().Error()
	} else if runErr != nil && stderr.buf.Len() == 0 {
		result["error"] = runErr.Error()
	}
	return mcpserver.ToolResult(result, runErr != nil), nil
}

func cleanupMCPContainers(requestID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "ps", "-aq", "--filter", "label=io.github.hquip.vsm.mcp="+requestID)
	data, err := cmd.Output()
	if err != nil {
		return
	}
	args := []string{"rm", "-f"}
	for _, id := range strings.Fields(string(data)) {
		if regexp.MustCompile(`^[0-9a-f]{12,64}$`).MatchString(id) {
			args = append(args, id)
		}
	}
	if len(args) > 2 {
		_ = exec.CommandContext(ctx, "docker", args...).Run()
	}
}

func sameMCPRoot(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

type cappedOutput struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (w *cappedOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := w.limit - w.buf.Len()
	if n > remaining {
		w.truncated = true
		p = p[:remaining]
	}
	_, err := w.buf.Write(p)
	return n, err
}
func (w *cappedOutput) String() string { return strings.ToValidUTF8(w.buf.String(), "�") }

var _ io.Writer = (*cappedOutput)(nil)
