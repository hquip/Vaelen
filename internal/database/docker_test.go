package database

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"vsm/internal/core"
)

func TestDockerRejectsForeignContainersAndVolumes(t *testing.T) {
	for _, foreign := range []string{"container", "volume"} {
		t.Run(foreign, func(t *testing.T) {
			paths := core.Paths{Root: t.TempDir()}
			t.Setenv("VSM_HOME", paths.Root)
			m := New(paths)
			if _, err := m.Use(context.Background(), Request{Name: "dev", Engine: "postgres", Version: "17"}); err != nil {
				t.Fatal(err)
			}
			i, _ := m.load("dev")
			digest := "sha256:" + strings.Repeat("a", 64)
			var calls []string
			m.runner = func(ctx context.Context, program string, args []string, env map[string]string) ([]byte, error) {
				command := strings.Join(args, " ")
				calls = append(calls, command)
				switch {
				case args[0] == "pull":
					return nil, nil
				case strings.HasPrefix(command, "image inspect"):
					return []byte(digest), nil
				case args[0] == "run" && args[1] == "--rm":
					return []byte("postgres (PostgreSQL) 17.9"), nil
				case strings.HasPrefix(command, "volume inspect"):
					owner := i.Owner
					if foreign == "volume" {
						owner = "not-ours"
					}
					data, _ := json.Marshal(map[string]string{ownerLabel: owner})
					return data, nil
				case strings.HasPrefix(command, "container inspect"):
					data, _ := json.Marshal(map[string]any{"Image": digest, "State": map[string]bool{"Running": false}, "Config": map[string]any{"Labels": map[string]string{ownerLabel: "not-ours", "io.github.hquip.vsm.database.version": "17"}}})
					return data, nil
				}
				return nil, fmt.Errorf("unexpected mutating command: %s", command)
			}
			if _, err := m.Start(context.Background(), "dev"); err == nil {
				t.Fatal("foreign resource accepted")
			}
			for _, call := range calls {
				if strings.HasPrefix(call, "container stop") || strings.HasPrefix(call, "container rm") || strings.Contains(call, "run --detach") {
					t.Fatalf("foreign resource changed: %s", call)
				}
			}
		})
	}
}
func TestDockerImageVersionVerifiedBeforeAllocatingData(t *testing.T) {
	paths := core.Paths{Root: t.TempDir()}
	t.Setenv("VSM_HOME", paths.Root)
	m := New(paths)
	if _, err := m.Use(context.Background(), Request{Name: "dev", Engine: "postgres", Version: "17"}); err != nil {
		t.Fatal(err)
	}
	m.runner = func(ctx context.Context, program string, args []string, env map[string]string) ([]byte, error) {
		switch args[0] {
		case "pull":
			return nil, nil
		case "image":
			return []byte("sha256:" + strings.Repeat("b", 64)), nil
		case "run":
			return []byte("postgres (PostgreSQL) 16.9"), nil
		}
		t.Fatalf("allocated data before version check: %v", args)
		return nil, nil
	}
	if _, err := m.Start(context.Background(), "dev"); err == nil || !strings.Contains(err.Error(), "实际") {
		t.Fatalf("mismatched image accepted: %v", err)
	}
	i, _ := m.load("dev")
	if i.Versions["17"].Allocated {
		t.Fatal("data allocated for wrong image")
	}
}
