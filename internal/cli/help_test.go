package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestEveryListedCommandHasDetailedHelp(t *testing.T) {
	_, commands, ok := strings.Cut(usageText, "\n命令:\n")
	if !ok {
		t.Fatal("missing command overview")
	}
	commands, _, _ = strings.Cut(commands, "\n  list /")
	for _, match := range regexp.MustCompile(`(?m)^  ([a-z][a-z-]*)\s`).FindAllStringSubmatch(commands, -1) {
		help, ok := commandHelpTopics[match[1]]
		if !ok || help.Summary == "" || len(help.Usage) == 0 || len(help.Examples) == 0 {
			t.Errorf("public command %q lacks usable detailed help", match[1])
		}
	}
	aliases := map[string]string{}
	for command, help := range commandHelpTopics {
		for _, alias := range help.Aliases {
			if previous, ok := aliases[alias]; ok {
				t.Fatalf("alias %q belongs to both %s and %s", alias, previous, command)
			}
			if _, ok := commandHelpTopics[alias]; ok {
				t.Fatalf("alias %q shadows a command", alias)
			}
			aliases[alias] = command
		}
	}
}

func TestCommandHelpDoesNotInitializeEnvironment(t *testing.T) {
	bin := buildVsm(t)
	blockedHome := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blockedHome, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	config := filepath.Join(cwd, ".vsm", "project.json")
	if err := os.MkdirAll(filepath.Dir(config), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte("broken project config"), 0644); err != nil {
		t.Fatal(err)
	}
	for topic, help := range commandHelpTopics {
		t.Run(topic, func(t *testing.T) {
			args := append([]string{"help"}, strings.Fields(topic)...)
			code, out := runVsm(t, bin, blockedHome, cwd, args...)
			if code != 0 || !strings.Contains(out, "vsm "+topic+" - ") || !strings.Contains(out, help.Summary) {
				t.Fatalf("help attempted initialization or showed the wrong topic: code=%d %s", code, out)
			}
			for _, section := range []string{"用法:", "选项:", "示例:"} {
				if !strings.Contains(out, section) {
					t.Fatalf("missing %s: %s", section, out)
				}
			}
			if !strings.Contains(topic, " ") {
				for _, alias := range help.Aliases {
					aliasCode, aliasOut := runVsm(t, bin, blockedHome, cwd, "help", alias)
					if aliasCode != 0 || aliasOut != out {
						t.Fatalf("alias %q did not select %s", alias, topic)
					}
				}
			}
		})
	}
	for _, args := range [][]string{
		{"help"}, {"--help"}, {"-h"}, {"install", "--help"}, {"i", "go", "-h"},
		{"project", "init", "--help"}, {"mcp", "config", "--client", "codex", "-h"},
		{"run", "--install-missing", "--help"}, {"exec", "--help"},
	} {
		if code, out := runVsm(t, bin, blockedHome, cwd, args...); code != 0 {
			t.Fatalf("help form %v failed: %s", args, out)
		}
	}
	for _, args := range [][]string{
		{"help", "unknown"}, {"help", "install", "node"}, {"help", "project", "unknown"}, {"help", "mcp", "unknown"}, {"unknown", "--help"},
	} {
		code, out := runVsm(t, bin, blockedHome, cwd, args...)
		if code == 0 || !strings.Contains(out, "未知帮助主题") {
			t.Fatalf("invalid topic %v did not fail clearly: %d %s", args, code, out)
		}
	}
	if data, err := os.ReadFile(config); err != nil || string(data) != "broken project config" {
		t.Fatal("help changed project configuration")
	}
	if data, err := os.ReadFile(blockedHome); err != nil || string(data) != "keep" {
		t.Fatal("help changed the data path")
	}
	if _, err := os.Stat(filepath.Join(cwd, ".tool-versions")); !os.IsNotExist(err) {
		t.Fatal("help wrote version configuration")
	}
}

func TestHelpFlagsReachChildCommands(t *testing.T) {
	bin := buildVsm(t)
	home, cwd := t.TempDir(), t.TempDir()
	child := filepath.Join(home, "installs", "node", "1.0.0", "bin", "node")
	if runtime.GOOS == "windows" {
		child = filepath.Join(home, "installs", "node", "1.0.0", "node.exe")
	}
	if err := os.MkdirAll(filepath.Dir(child), 0755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(child, data, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".tool-versions"), []byte("node 1.0.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// A copy of VSM plays the child tool: a forwarded flag prints its general
	// command overview, while interception prints exec/run-specific help.
	for _, args := range [][]string{
		{"exec", "node", "--help"}, {"x", "node", "-h"}, {"exec", "node", "--", "node", "--help"},
		{"run", "--", "node", "--help"}, {"run", "--", "node", "-h"},
	} {
		code, out := runVsm(t, bin, home, cwd, args...)
		if code != 0 || !strings.Contains(out, "vsm <命令> [参数]") {
			t.Fatalf("child help was intercepted for %v: %d %s", args, code, out)
		}
	}
}
