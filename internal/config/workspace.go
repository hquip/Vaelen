package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const WorkspaceName = "vsm.json"

type Workspace struct {
	Schema   int               `json:"schema_version"`
	Tools    map[string]string `json:"tools,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	EnvFiles []string          `json:"env_files,omitempty"`
	Tasks    map[string]Task   `json:"tasks,omitempty"`
}

type Task struct {
	Description    string            `json:"description,omitempty"`
	Command        []string          `json:"command,omitempty"`
	Depends        []string          `json:"depends,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	Dir            string            `json:"dir,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
	Inputs         []string          `json:"inputs,omitempty"`
	Outputs        []string          `json:"outputs,omitempty"`
}

var identifier = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_.+-]*$`)
var envIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// DecodeStrict rejects unknown fields, duplicate keys and trailing documents.
func DecodeStrict(data []byte, out any) error {
	data = bytes.TrimPrefix(data, BOM)
	check := json.NewDecoder(bytes.NewReader(data))
	var walk func() error
	walk = func() error {
		t, err := check.Token()
		if err != nil {
			return err
		}
		d, ok := t.(json.Delim)
		if !ok {
			return nil
		}
		if d == '{' {
			seen := map[string]bool{}
			for check.More() {
				k, err := check.Token()
				if err != nil {
					return err
				}
				key := k.(string)
				if seen[key] {
					return fmt.Errorf("duplicate JSON key %q", key)
				}
				seen[key] = true
				if err := walk(); err != nil {
					return err
				}
			}
		} else if d == '[' {
			for check.More() {
				if err := walk(); err != nil {
					return err
				}
			}
		} else {
			return fmt.Errorf("invalid JSON delimiter")
		}
		_, err = check.Token()
		return err
	}
	if err := walk(); err != nil {
		return err
	}
	if _, err := check.Token(); err != io.EOF {
		return fmt.Errorf("expected exactly one JSON document")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(out)
}

func ReadBounded(path string, limit int64) ([]byte, error) {
	f, err := os.OpenInRoot(filepath.Dir(path), filepath.Base(path))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("configuration exceeds %d bytes", limit)
	}
	return data, nil
}

func LoadWorkspace(path string) (*Workspace, error) {
	data, err := ReadBounded(path, 1<<20)
	if err != nil {
		return nil, err
	}
	var c Workspace
	if err := DecodeStrict(data, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &c, nil
}

func ValidUserEnv(key, value string) bool {
	u := strings.ToUpper(key)
	if !envIdentifier.MatchString(key) || strings.ContainsAny(value, "\x00\r\n") {
		return false
	}
	// Toolchain state and process-launch controls must not override isolation.
	for _, prefix := range []string{"VSM_", "_VSM_", "LD_", "DYLD_"} {
		if strings.HasPrefix(u, prefix) {
			return false
		}
	}
	switch u {
	case "PATH", "GOROOT", "GOPATH", "GOBIN", "GOCACHE", "GOMODCACHE", "GOTOOLCHAIN", "GOENV", "GOWORK", "JAVA_HOME", "VIRTUAL_ENV", "PYTHONHOME", "PYTHONPATH", "PYTHONNOUSERSITE", "PIP_CACHE_DIR", "PIP_REQUIRE_VIRTUALENV", "CARGO_HOME", "RUSTUP_HOME", "GRADLE_USER_HOME", "NPM_CONFIG_CACHE", "NPM_CONFIG_PREFIX", "NPM_CONFIG_USERCONFIG", "NODE_PATH", "COMSPEC", "SYSTEMROOT", "PATHEXT":
		return false
	}
	return true
}

func localPath(path string) bool {
	return path != "" && !strings.ContainsAny(path, "\\:\x00\r\n") && filepath.IsLocal(filepath.FromSlash(path))
}

func (c Workspace) Validate() error {
	if c.Schema != 1 {
		return fmt.Errorf("unsupported vsm.json schema_version")
	}
	if len(c.Tools) > 256 || len(c.Tasks) > 128 || len(c.Env) > 256 || len(c.EnvFiles) > 16 {
		return fmt.Errorf("project configuration exceeds entry limits")
	}
	for tool, spec := range c.Tools {
		if !identifier.MatchString(tool) || spec == "" || strings.ContainsAny(spec, "\x00\r\n") {
			return fmt.Errorf("invalid tool requirement %q", tool)
		}
	}
	for k, v := range c.Env {
		if !ValidUserEnv(k, v) {
			return fmt.Errorf("unsafe project environment key %q", k)
		}
	}
	for _, path := range c.EnvFiles {
		if !localPath(path) {
			return fmt.Errorf("env file must stay within project: %q", path)
		}
	}
	for name, task := range c.Tasks {
		if !identifier.MatchString(name) || len(task.Command) > 256 || len(task.Depends) > 128 || task.TimeoutSeconds < 0 || task.TimeoutSeconds > 86400 {
			return fmt.Errorf("invalid task %q", name)
		}
		if len(task.Command) == 0 && len(task.Depends) == 0 {
			return fmt.Errorf("task %s needs command or depends", name)
		}
		for _, arg := range task.Command {
			if strings.ContainsRune(arg, 0) {
				return fmt.Errorf("task %s contains NUL", name)
			}
		}
		if len(task.Command) > 0 && (task.Command[0] == "" || strings.ContainsAny(task.Command[0], "/\\:\r\n")) {
			return fmt.Errorf("task %s command must be a program name", name)
		}
		if task.Dir != "" && !localPath(task.Dir) {
			return fmt.Errorf("task %s dir escapes project", name)
		}
		for k, v := range task.Env {
			if !ValidUserEnv(k, v) {
				return fmt.Errorf("unsafe task environment key %q", k)
			}
		}
		if len(task.Inputs) > 128 || len(task.Outputs) > 128 {
			return fmt.Errorf("task cache pattern limit exceeded")
		}
		for _, p := range append(append([]string{}, task.Inputs...), task.Outputs...) {
			if !localPath(p) {
				return fmt.Errorf("task %s cache path escapes project", name)
			}
		}
	}
	state := map[string]int{}
	var visit func(string) error
	visit = func(name string) error {
		if state[name] == 1 {
			return fmt.Errorf("task dependency cycle at %s", name)
		}
		if state[name] == 2 {
			return nil
		}
		task, ok := c.Tasks[name]
		if !ok {
			return fmt.Errorf("unknown dependency %s", name)
		}
		state[name] = 1
		for _, dep := range task.Depends {
			if err := visit(dep); err != nil {
				return err
			}
		}
		state[name] = 2
		return nil
	}
	for name := range c.Tasks {
		if err := visit(name); err != nil {
			return err
		}
	}
	return nil
}

func (c Workspace) Save(path string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer root.Close()
	return WriteRootAtomic(root, filepath.Base(path), append(data, '\n'), 0o644)
}
