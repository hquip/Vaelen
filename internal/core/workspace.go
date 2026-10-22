package core

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"vsm/internal/config"
	"vsm/internal/plugin"
)

type Workspace struct {
	Root   string            `json:"root"`
	File   string            `json:"file"`
	Config *config.Workspace `json:"config"`
}

// WorkspaceConfigurationPath locates configuration without parsing it, so the
// GUI can show and repair an invalid document without executing any project code.
func WorkspaceConfigurationPath(start string) (string, error) {
	project, err := FindProject(start)
	if err != nil {
		return "", err
	}
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		file := filepath.Join(dir, config.WorkspaceName)
		if _, err := os.Lstat(file); err == nil {
			return file, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		if project != nil && sameDir(project.Root, dir) {
			return "", os.ErrNotExist
		}
		if boundary := os.Getenv("VSM_PROJECT_BOUNDARY"); boundary != "" && sameDir(boundary, dir) {
			return "", os.ErrNotExist
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}

func FindWorkspace(start string) (*Workspace, error) {
	return findWorkspaceBounded(start, os.Getenv("VSM_PROJECT_BOUNDARY"))
}

func findWorkspaceBounded(start, boundary string) (*Workspace, error) {
	return newResolutionScan(start, boundary).findWorkspace()
}

func SetProjectVersion(dir, tool, version string) error {
	workspace, err := FindWorkspace(dir)
	if err != nil {
		return err
	}
	if workspace != nil && sameDir(workspace.Root, dir) {
		if workspace.Config.Tools == nil {
			workspace.Config.Tools = map[string]string{}
		}
		workspace.Config.Tools[tool] = version
		return workspace.Config.Save(workspace.File)
	}
	return config.SetIn(filepath.Join(dir, ".tool-versions"), tool, version)
}

func InitializeWorkspace(dir string) (*Workspace, error) {
	root, err := ProjectDirectory(dir)
	if err != nil {
		return nil, err
	}
	file := filepath.Join(root, config.WorkspaceName)
	if _, err := os.Lstat(file); err == nil {
		return FindWorkspace(root)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	c := &config.Workspace{Schema: 1, Tools: map[string]string{}, Env: map[string]string{}, Tasks: map[string]config.Task{}}
	for _, tool := range plugin.Names() {
		spec, src, err := ProjectRequirement(tool, root)
		if err != nil {
			return nil, err
		}
		if src != "" {
			c.Tools[tool] = spec
		}
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
		c.Tasks["test"] = config.Task{Command: []string{"go", "test", "./..."}}
		c.Tasks["build"] = config.Task{Command: []string{"go", "build", "./..."}}
	}
	if len(c.Tasks) == 0 {
		if _, err := os.Stat(filepath.Join(root, "package.json")); err == nil {
			c.Tasks["test"] = config.Task{Command: []string{"npm", "test"}}
			c.Tasks["build"] = config.Task{Command: []string{"npm", "run", "build"}}
		}
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	_, err = f.Write(append(data, '\n'))
	closeErr := f.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return &Workspace{Root: root, File: file, Config: c}, nil
}

func (w Workspace) inputDigest() (string, error) {
	digest, _, err := w.readInputs()
	return digest, err
}
func (w Workspace) readInputs() (string, map[string][]byte, error) {
	data, err := config.ReadBounded(w.File, 1<<20)
	if err != nil {
		return "", nil, err
	}
	var current config.Workspace
	if err := config.DecodeStrict(data, &current); err != nil {
		return "", nil, err
	}
	expected, _ := json.Marshal(w.Config)
	actual, _ := json.Marshal(current)
	if !bytes.Equal(expected, actual) {
		return "", nil, fmt.Errorf("项目配置在操作期间发生变化，请刷新后重试")
	}
	h := sha256.New()
	h.Write([]byte("vsm-workspace-trust-v2\n"))
	// Length framing prevents file contents from impersonating the separators
	// and names of other inputs (dotenv comments can contain arbitrary bytes).
	writeInput := func(name string, content []byte) {
		_ = binary.Write(h, binary.BigEndian, uint64(len(name)))
		h.Write([]byte(name))
		_ = binary.Write(h, binary.BigEndian, uint64(len(content)))
		h.Write(content)
	}
	writeInput(config.WorkspaceName, data)
	guard, err := os.OpenRoot(w.Root)
	if err != nil {
		return "", nil, err
	}
	defer guard.Close()
	files := map[string][]byte{}
	for _, name := range w.Config.EnvFiles {
		f, err := guard.Open(filepath.FromSlash(name))
		if err != nil {
			return "", nil, fmt.Errorf("env file %s: %w", name, err)
		}
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
			f.Close()
			return "", nil, fmt.Errorf("invalid env file %s", name)
		}
		value, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
		f.Close()
		if err != nil {
			return "", nil, err
		}
		if len(value) > 1<<20 {
			return "", nil, fmt.Errorf("env file too large")
		}
		files[name] = value
		writeInput(name, value)
	}
	return hex.EncodeToString(h.Sum(nil)), files, nil
}

func (w Workspace) trustFile() (string, error) {
	global, err := DefaultPaths()
	if err != nil {
		return "", err
	}
	root, err := canonicalPath(w.Root)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(root))
	return filepath.Join(global.Root, "trust", hex.EncodeToString(sum[:])+".json"), nil
}

func (w Workspace) trustedDigest(digest string) bool {
	file, err := w.trustFile()
	if err != nil {
		return false
	}
	data, err := config.ReadBounded(file, 4096)
	if err != nil {
		return false
	}
	var receipt struct {
		Digest string `json:"digest"`
	}
	return config.DecodeStrict(data, &receipt) == nil && receipt.Digest == digest
}
func (w Workspace) Trusted() bool {
	digest, err := w.inputDigest()
	return err == nil && w.trustedDigest(digest)
}

func (w Workspace) Trust() error {
	if os.Getenv("VSM_MCP_REQUEST_ID") != "" {
		return fmt.Errorf("MCP cannot grant project trust; review configuration in the CLI or GUI")
	}
	digest, err := w.inputDigest()
	if err != nil {
		return err
	}
	file, err := w.trustFile()
	if err != nil {
		return err
	}
	data, _ := json.Marshal(map[string]string{"digest": digest})
	return config.WriteFileAtomic(file, data, 0o600)
}

func (w Workspace) Environment() (map[string]string, error) {
	env := map[string]string{}
	if len(w.Config.Env) == 0 && len(w.Config.EnvFiles) == 0 {
		return env, nil
	}
	digest, files, err := w.readInputs()
	if err != nil {
		return nil, err
	}
	if !w.trustedDigest(digest) {
		return nil, fmt.Errorf("项目变量尚未信任或已变更；检查 vsm.json 和 env_files 后运行 vsm project trust")
	}
	for _, name := range w.Config.EnvFiles {
		scan := bufio.NewScanner(bytes.NewReader(files[name]))
		scan.Buffer(make([]byte, 4096), 1<<20)
		for scan.Scan() {
			line := strings.TrimSpace(scan.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			line = strings.TrimPrefix(line, "export ")
			key, value, ok := strings.Cut(line, "=")
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			if len(value) >= 2 && ((value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"')) {
				value = value[1 : len(value)-1]
			}
			if !ok || !config.ValidUserEnv(key, value) {
				return nil, fmt.Errorf("invalid dotenv key in %s", name)
			}
			env[key] = value
		}
		if err := scan.Err(); err != nil {
			return nil, err
		}
	}
	for k, v := range w.Config.Env {
		env[k] = v
	}
	return env, nil
}

func (w Workspace) EnvironmentKeys() []string {
	keys := []string{}
	for k := range w.Config.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
