package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

type testClient struct {
	encode    *json.Encoder
	responses chan map[string]any
	input     *io.PipeWriter
	cancel    context.CancelFunc
	done      chan error
}

func startTestServer(t *testing.T, call func(context.Context, string, json.RawMessage) (Result, error)) *testClient {
	t.Helper()
	in, inW := io.Pipe()
	outR, out := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	c := &testClient{json.NewEncoder(inW), make(chan map[string]any, 16), inW, cancel, make(chan error, 1)}
	server := Server{Version: "test", Tools: []Tool{{Name: "test", InputSchema: map[string]any{"type": "object"}}}, Call: call}
	go func() { c.done <- server.Serve(ctx, in, out); out.Close() }()
	go func() {
		decoder := json.NewDecoder(outR)
		for {
			var response map[string]any
			if decoder.Decode(&response) != nil {
				return
			}
			c.responses <- response
		}
	}()
	t.Cleanup(func() {
		cancel()
		inW.Close()
		select {
		case <-c.done:
		case <-time.After(5 * time.Second):
			t.Error("MCP did not shut down")
		}
		outR.Close()
	})
	return c
}
func (c *testClient) send(t *testing.T, id any, method string, params any) {
	t.Helper()
	m := map[string]any{"jsonrpc": "2.0", "method": method}
	if id != nil {
		m["id"] = id
	}
	if params != nil {
		m["params"] = params
	}
	if err := c.encode.Encode(m); err != nil {
		t.Fatal(err)
	}
}
func (c *testClient) read(t *testing.T) map[string]any {
	t.Helper()
	select {
	case result := <-c.responses:
		return result
	case <-time.After(8 * time.Second):
		t.Fatal("MCP response timed out")
		return nil
	}
}
func (c *testClient) init(t *testing.T) {
	c.send(t, 1, "initialize", map[string]any{"protocolVersion": "2025-11-25", "clientInfo": map[string]any{"name": "test", "version": "1"}, "capabilities": map[string]any{}})
	if c.read(t)["error"] != nil {
		t.Fatal("initialize failed")
	}
	c.send(t, nil, "notifications/initialized", nil)
}

func TestLifecycleAndToolResults(t *testing.T) {
	c := startTestServer(t, func(ctx context.Context, name string, args json.RawMessage) (Result, error) {
		return ToolResult(map[string]any{"value": "line one\nline two"}, false), nil
	})
	c.send(t, 0, "tools/list", nil)
	if c.read(t)["error"] == nil {
		t.Fatal("tools allowed before initialization")
	}
	c.init(t)
	c.send(t, 2, "tools/list", nil)
	if result := c.read(t); result["error"] != nil {
		t.Fatal(result)
	}
	c.send(t, "call", "tools/call", map[string]any{"name": "test", "arguments": map[string]any{}})
	response := c.read(t)
	if response["id"] != "call" || response["error"] != nil {
		t.Fatal(response)
	}
	result := response["result"].(map[string]any)
	if result["structuredContent"].(map[string]any)["value"] != "line one\nline two" {
		t.Fatal("structured result missing")
	}
	c.send(t, 4, "tools/call", map[string]any{"name": "unknown"})
	if c.read(t)["error"] == nil {
		t.Fatal("unknown tool accepted")
	}
}

func TestCancellationDoesNotBlockProtocol(t *testing.T) {
	started := make(chan struct{})
	c := startTestServer(t, func(ctx context.Context, name string, args json.RawMessage) (Result, error) {
		close(started)
		<-ctx.Done()
		return Result{}, ctx.Err()
	})
	c.init(t)
	c.send(t, 2, "tools/call", map[string]any{"name": "test"})
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("call never started")
	}
	c.send(t, nil, "notifications/cancelled", map[string]any{"requestId": 2})
	response := c.read(t)
	result := response["result"].(map[string]any)
	if result["isError"] != true {
		t.Fatal("cancelled call was not a tool error")
	}
	c.send(t, 3, "ping", nil)
	if c.read(t)["id"] != float64(3) {
		t.Fatal("server stopped responding")
	}
}

func TestMalformedJSONIsProtocolError(t *testing.T) {
	c := startTestServer(t, nil)
	if _, err := io.WriteString(c.input, "{bad json}\n"); err != nil {
		t.Fatal(err)
	}
	response := c.read(t)
	if response["error"].(map[string]any)["code"] != float64(-32700) {
		t.Fatal(response)
	}
	if strings.Contains(response["error"].(map[string]any)["message"].(string), "panic") {
		t.Fatal("unexpected panic")
	}
}
