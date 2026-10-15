package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestMCPManagesOnlyItsBoundProject(t *testing.T) {
	bin := buildVsm(t)
	project, other, global := t.TempDir(), t.TempDir(), t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "mcp", "serve", "--project", project)
	cmd.Env = append(os.Environ(), "VSM_HOME="+global)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stdin.Close(); cancel(); cmd.Wait() })
	responses := make(chan map[string]any, 8)
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 4096), 2<<20)
		for scanner.Scan() {
			var response map[string]any
			if json.Unmarshal(scanner.Bytes(), &response) != nil {
				responses <- map[string]any{"bad_stdout": scanner.Text()}
				return
			}
			responses <- response
		}
	}()
	encoder := json.NewEncoder(stdin)
	id := 0
	read := func() map[string]any {
		select {
		case response := <-responses:
			return response
		case <-time.After(20 * time.Second):
			t.Fatal("MCP subprocess did not respond")
			return nil
		}
	}
	request := func(method string, params any) map[string]any {
		id++
		if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
			t.Fatal(err)
		}
		result := read()
		if result["error"] != nil || result["bad_stdout"] != nil {
			t.Fatalf("protocol error: %v", result)
		}
		return result["result"].(map[string]any)
	}
	request("initialize", map[string]any{"protocolVersion": "2025-11-25", "clientInfo": map[string]any{"name": "integration-test", "version": "1"}, "capabilities": map[string]any{}})
	if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); err != nil {
		t.Fatal(err)
	}
	list := request("tools/list", map[string]any{})
	if len(list["tools"].([]any)) != 7 {
		t.Fatalf("unexpected tools: %v", list)
	}
	call := func(name string, args map[string]any) map[string]any {
		return request("tools/call", map[string]any{"name": name, "arguments": args})
	}
	if result := call("vsm_project_init", map[string]any{}); result["isError"] == true {
		t.Fatalf("init failed: %v", result)
	}
	if _, err := os.Stat(filepath.Join(project, ".vsm", "project.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(other, ".vsm")); !os.IsNotExist(err) {
		t.Fatal("another project was modified")
	}
	if result := call("vsm_use", map[string]any{"tool": "node", "version": "1.0.0"}); result["isError"] == true {
		t.Fatal(result)
	}
	if result := call("vsm_use", map[string]any{"tool": "node", "version": "2.0.0", "project": other}); result["isError"] != true {
		t.Fatal("accepted project redirection")
	}
	status := call("vsm_context", map[string]any{})
	if status["isError"] == true {
		t.Fatal(status)
	}
	if _, err := os.Stat(filepath.Join(global, ".tool-versions")); !os.IsNotExist(err) {
		t.Fatal("MCP altered global versions")
	}
	// An actual child proves execution uses project paths and preserves argv.
	src := filepath.Join(t.TempDir(), "main.go")
	source := `package main
import("encoding/json";"os";"time")
func main(){if len(os.Args)>1&&os.Args[1]=="sleep"{time.Sleep(15*time.Second)};json.NewEncoder(os.Stdout).Encode(map[string]any{"root":os.Getenv("VSM_PROJECT_ROOT"),"cache":os.Getenv("npm_config_cache"),"args":os.Args[1:]})}`
	if err := os.WriteFile(src, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(project, ".vsm", "env", "installs", "node", "1.0.0", "bin", "node")
	if runtime.GOOS == "windows" {
		target = filepath.Join(project, ".vsm", "env", "installs", "node", "1.0.0", "node.exe")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", target, src)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, out)
	}
	result := call("vsm_run", map[string]any{"command": "node", "args": []string{"a; literal & argument"}})
	if result["isError"] == true {
		t.Fatal(result)
	}
	structured := result["structuredContent"].(map[string]any)
	var child struct {
		Root  string   `json:"root"`
		Cache string   `json:"cache"`
		Args  []string `json:"args"`
	}
	if err := json.Unmarshal([]byte(structured["stdout"].(string)), &child); err != nil {
		t.Fatal(err, structured)
	}
	if len(child.Args) != 1 || child.Args[0] != "a; literal & argument" || !strings.Contains(child.Cache, ".vsm") {
		t.Fatalf("command output missing: %+v", child)
	}
	start := time.Now()
	cancelled := call("vsm_run", map[string]any{"command": "node", "args": []string{"sleep"}, "timeout_seconds": 1})
	if cancelled["isError"] != true || time.Since(start) > 8*time.Second {
		t.Fatal("MCP command timeout did not stop the process tree")
	}
}

func TestReadOnlyMCPHasNoMutationTools(t *testing.T) {
	for _, tool := range mcpTools(true) {
		if tool.Annotations["readOnlyHint"] != true {
			t.Fatal("read-only mode exposes a mutation")
		}
	}
}
