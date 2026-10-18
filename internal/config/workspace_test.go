package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceRejectsAmbiguousAndUnsafeConfiguration(t *testing.T) {
	for _, raw := range []string{
		`{"schema_version":1,"tools":{"go":"1","go":"2"}}`,
		`{"schema_version":1,"unknown":true}`,
		`{"schema_version":1} {}`,
		`{"schema_version":1,"env":{"PATH":"/outside"}}`,
		`{"schema_version":1,"env":{"GOPATH":"/outside"}}`,
		`{"schema_version":1,"env_files":["../secret"]}`,
		`{"schema_version":1,"tasks":{"a":{"depends":["b"]},"b":{"depends":["a"]}}}`,
		`{"schema_version":1,"tasks":{"a":{"depends":["missing"]}}}`,
		`{"schema_version":1,"tasks":{"a":{"command":["go"],"dir":"../outside"}}}`,
	} {
		path := filepath.Join(t.TempDir(), WorkspaceName)
		if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadWorkspace(path); err == nil {
			t.Fatalf("unsafe config accepted: %s", raw)
		}
	}
}
