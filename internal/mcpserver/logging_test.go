package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"vsm/internal/logging"
)

func TestMCPLogsNeverContainRequestOrOutput(t *testing.T) {
	t.Setenv("VSM_HOME", t.TempDir())
	t.Setenv("VSM_LOG_LEVEL", "debug")
	client := startTestServer(t, func(context.Context, string, json.RawMessage) (Result, error) {
		return ToolResult(map[string]any{"output": "private-command-output"}, true), nil
	})
	client.init(t)
	client.send(t, 2, "tools/call", map[string]any{"name": "test", "arguments": map[string]any{"command": "private-command-argument"}})
	response := client.read(t)
	if response["jsonrpc"] != "2.0" || response["result"] == nil {
		t.Fatalf("MCP stream corrupted: %v", response)
	}
	store, err := logging.Default()
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		snapshot, err := store.Read(100, "")
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(snapshot)
		if strings.Contains(string(raw), "private-command") {
			t.Fatal("request/output leaked into logs")
		}
		for _, entry := range snapshot.Entries {
			if entry.Event == "tool.failed" {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("tool failure was not logged")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
