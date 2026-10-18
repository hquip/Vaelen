package core

import (
	"os"
	"path/filepath"
	"testing"

	"vsm/internal/config"
)

func TestWorkspaceTrustDigestUnambiguouslyFramesEnvironmentFiles(t *testing.T) {
	t.Setenv("VSM_HOME", t.TempDir())
	t.Setenv("VSM_MCP_REQUEST_ID", "")
	t.Setenv("VSM_PROJECT_BOUNDARY", "")
	root := t.TempDir()
	c := config.Workspace{Schema: 1, EnvFiles: []string{"a.env", "b.env"}}
	if err := c.Save(filepath.Join(root, config.WorkspaceName)); err != nil {
		t.Fatal(err)
	}
	writeEnv := func(name, contents string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Comments are ignored by the dotenv reader, including embedded NUL bytes.
	// Moving bytes across file boundaries must still invalidate project trust.
	writeEnv("a.env", "#x")
	writeEnv("b.env", "KEY=foo\n#\x00b.env\x00KEY=bar")
	w, err := FindWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Trust(); err != nil {
		t.Fatal(err)
	}
	before, err := w.Environment()
	if err != nil || before["KEY"] != "foo" {
		t.Fatalf("initial trusted environment: %v %v", before, err)
	}
	writeEnv("a.env", "#x\x00b.env\x00KEY=foo\n#")
	writeEnv("b.env", "KEY=bar")
	if w.Trusted() {
		t.Fatal("environment file changes preserved trust by moving NUL-delimited bytes between files")
	}
	if after, err := w.Environment(); err == nil {
		t.Fatalf("changed environment accepted without renewed trust: %v", after)
	}
}
