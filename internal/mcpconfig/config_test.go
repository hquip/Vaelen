package mcpconfig

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestClientsPreserveExecutableAndBoundArguments(t *testing.T) {
	exe := `C:\Program Files\VSM\vsm.exe`
	args := []string{"mcp", "serve", "--project", "D:\\项目 & space\\a\"b\n\t\x7f", "--read-only"}
	for _, profile := range Clients("windows") {
		t.Run(profile.ID, func(t *testing.T) {
			result, err := Generate(profile.ID, "windows", exe, args)
			if err != nil {
				t.Fatal(err)
			}
			if result.Client != profile || result.Client.Location == "" || result.Client.Instructions == "" {
				t.Fatalf("missing client guidance: %+v", result.Client)
			}
			var command string
			var decodedArgs []string
			if profile.Format == "toml" {
				if !strings.HasPrefix(result.Content, "[mcp_servers.vsm]\n") {
					t.Fatal("Codex configuration uses the wrong table")
				}
				for _, line := range strings.Split(result.Content, "\n") {
					if value, ok := strings.CutPrefix(line, "command = "); ok {
						if err := json.Unmarshal([]byte(value), &command); err != nil {
							t.Fatal(err)
						}
					}
					if value, ok := strings.CutPrefix(line, "args = "); ok {
						if err := json.Unmarshal([]byte(value), &decodedArgs); err != nil {
							t.Fatal(err)
						}
					}
				}
			} else {
				var envelope map[string]map[string]struct {
					Command string   `json:"command"`
					Args    []string `json:"args"`
					Type    string   `json:"type"`
				}
				if err := json.Unmarshal([]byte(result.Content), &envelope); err != nil {
					t.Fatal(err)
				}
				key := "mcpServers"
				if profile.ID == "vscode" {
					key = "servers"
				}
				if len(envelope) != 1 || len(envelope[key]) != 1 {
					t.Fatalf("wrong client schema: %s", result.Content)
				}
				server := envelope[key]["vsm"]
				command, decodedArgs = server.Command, server.Args
				if profile.ID == "vscode" && server.Type != "stdio" {
					t.Fatal("VS Code requires stdio type")
				}
			}
			if command != exe || !reflect.DeepEqual(decodedArgs, args) {
				t.Fatalf("paths or read-only arguments changed: %q, %#v", command, decodedArgs)
			}
		})
	}
}

func TestClientValidationAndPlatformLocations(t *testing.T) {
	if _, err := Generate("unknown", "windows", "vsm.exe", nil); err == nil {
		t.Fatal("unknown client was accepted")
	}
	if _, err := Generate("codex", "darwin", "", nil); err == nil {
		t.Fatal("empty executable was accepted")
	}
	for _, platform := range []string{"windows", "darwin", "linux"} {
		seen := map[string]bool{}
		for _, profile := range Clients(platform) {
			if seen[profile.ID] {
				t.Fatal("duplicate client ID")
			}
			seen[profile.ID] = true
			if profile.ID == "claude-desktop" {
				if platform == "windows" && !strings.Contains(profile.Location, "%APPDATA%") {
					t.Fatal("wrong Windows Desktop config location")
				}
				if platform == "darwin" && !strings.Contains(profile.Location, "Library/Application Support") {
					t.Fatal("wrong macOS Desktop config location")
				}
			}
		}
		if len(seen) != 7 {
			t.Fatalf("expected six client profiles and a generic profile: %v", seen)
		}
	}
}
