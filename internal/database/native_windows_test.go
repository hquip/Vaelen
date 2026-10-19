package database

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMariaDBWindowsBootstrapUsesSupportedOptions(t *testing.T) {
	m, _ := fakeManager(t)
	configureTest(t, m)
	i, _ := m.load("dev")
	i.Backend = "native"
	i.Engine = "mariadb"
	if err := m.ensure("instances", i.Name, "versions", i.Backend+"-"+i.Version, "data"); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(m.Tools.InstallDir("mariadb", i.Version), "bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "mariadb-install-db.exe"), []byte("fixture"), 0755); err != nil {
		t.Fatal(err)
	}
	called := false
	m.runner = func(ctx context.Context, program string, args []string, env map[string]string) ([]byte, error) {
		called = true
		if len(args) != 1 || !strings.HasPrefix(args[0], "--datadir=") {
			t.Fatalf("unsupported Windows bootstrap options or plaintext password: %v", args)
		}
		return nil, nil
	}
	if err := (nativeDriver{m}).initialize(context.Background(), i, credential{Password: "private-password"}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("bootstrap not executed")
	}
}
