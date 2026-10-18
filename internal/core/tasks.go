package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"vsm/internal/buildinfo"
	"vsm/internal/config"
	"vsm/internal/logging"
	"vsm/internal/mcpserver"
	"vsm/internal/plugin"
)

type TaskPlan struct {
	Root    string        `json:"root"`
	Trusted bool          `json:"trusted"`
	Tasks   []PlannedTask `json:"tasks"`
}
type PlannedTask struct {
	Name       string      `json:"name"`
	Definition config.Task `json:"definition"`
}

func (t PlannedTask) MarshalJSON() ([]byte, error) {
	copy := t.Definition
	if len(copy.Env) > 0 {
		copy.Env = map[string]string{}
		for key := range t.Definition.Env {
			copy.Env[key] = "[REDACTED]"
		}
	}
	type public struct {
		Name       string      `json:"name"`
		Definition config.Task `json:"definition"`
	}
	return json.Marshal(public{t.Name, copy})
}

type TaskEvent struct {
	Task   string `json:"task"`
	State  string `json:"state"`
	Output string `json:"output,omitempty"`
}
type TaskResult struct {
	Name       string `json:"name"`
	State      string `json:"state"`
	ExitCode   int    `json:"exit_code"`
	DurationMS int64  `json:"duration_ms"`
	Output     string `json:"output,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
	Error      string `json:"error,omitempty"`
}
type TaskReport struct {
	Root    string       `json:"root"`
	Tasks   []TaskResult `json:"tasks"`
	Success bool         `json:"success"`
}
type TaskOptions struct {
	RequestID string
	Jobs      int
	NoCache   bool
	Locked    bool
	Args      []string
	OnEvent   func(TaskEvent)
}
type TaskRunError struct {
	Code    int
	Message string
}

func (e TaskRunError) Error() string { return e.Message }
func (e TaskRunError) ExitCode() int { return e.Code }

func PlanTasks(dir string, names []string) (TaskPlan, error) {
	w, err := FindWorkspace(dir)
	if err != nil {
		return TaskPlan{}, err
	}
	if w == nil {
		return TaskPlan{}, fmt.Errorf("没有 vsm.json；运行 vsm project configure")
	}
	plan := TaskPlan{Root: w.Root, Trusted: w.Trusted(), Tasks: []PlannedTask{}}
	if len(names) == 0 {
		for name := range w.Config.Tasks {
			names = append(names, name)
		}
		sort.Strings(names)
	}
	seen := map[string]bool{}
	var add func(string) error
	add = func(name string) error {
		if seen[name] {
			return nil
		}
		task, ok := w.Config.Tasks[name]
		if !ok {
			return fmt.Errorf("未知任务 %s", name)
		}
		for _, dep := range task.Depends {
			if err := add(dep); err != nil {
				return err
			}
		}
		seen[name] = true
		plan.Tasks = append(plan.Tasks, PlannedTask{name, task})
		return nil
	}
	for _, name := range names {
		if err := add(name); err != nil {
			return plan, err
		}
	}
	return plan, nil
}

func RunTasks(parent context.Context, dir, executable string, names []string, options TaskOptions) (TaskReport, error) {
	plan, err := PlanTasks(dir, names)
	report := TaskReport{Root: plan.Root, Tasks: []TaskResult{}}
	if err != nil {
		return report, err
	}
	if len(names) == 0 {
		return report, fmt.Errorf("至少指定一个任务")
	}
	if !plan.Trusted {
		return report, fmt.Errorf("任务配置尚未信任或已变更；检查 vsm.json 后运行 vsm project trust")
	}
	if options.Locked {
		lock, err := LoadLock(dir)
		if err != nil {
			return report, err
		}
		if err := lock.validateRequirementsBounded(dir, plan.Root); err != nil {
			return report, err
		}
		global, err := DefaultPaths()
		if err != nil {
			return report, err
		}
		paths, err := taskPaths(global, dir, plan.Root)
		if err != nil {
			return report, err
		}
		target := plugin.CurrentTarget().OS + "/" + plugin.CurrentTarget().Arch
		for tool, entry := range lock.Tools {
			artifact, ok := entry.Platforms[target]
			if !ok {
				return report, fmt.Errorf("lock is missing platform %s", target)
			}
			if err := CheckInstalledArtifact(paths.InstallDir(tool, entry.Version), artifact); err != nil {
				return report, fmt.Errorf("%s: %w", tool, err)
			}
		}
	}
	w, err := FindWorkspace(dir)
	if err != nil {
		return report, err
	}
	if _, err := w.Environment(); err != nil {
		return report, err
	}
	if options.Jobs == 0 {
		options.Jobs = 4
	}
	if options.Jobs < 1 || options.Jobs > 16 {
		return report, fmt.Errorf("jobs 必须为 1–16")
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	var events sync.Mutex
	emit := func(event TaskEvent) {
		events.Lock()
		defer events.Unlock()
		if options.OnEvent != nil {
			options.OnEvent(event)
		}
	}
	results := map[string]TaskResult{}
	pending := map[string]PlannedTask{}
	for _, task := range plan.Tasks {
		pending[task.Name] = task
	}
	rootTasks := map[string]bool{}
	for _, name := range names {
		rootTasks[name] = true
	}
	failedCode := 0
	for len(pending) > 0 {
		ready := []PlannedTask{}
		for _, task := range plan.Tasks {
			if _, ok := pending[task.Name]; !ok {
				continue
			}
			eligible := true
			for _, dep := range task.Definition.Depends {
				r, ok := results[dep]
				if !ok || (r.State != "success" && r.State != "cached") {
					eligible = false
					break
				}
			}
			if eligible {
				ready = append(ready, task)
			}
		}
		if len(ready) == 0 || ctx.Err() != nil {
			break
		}
		sem := make(chan struct{}, options.Jobs)
		var wg sync.WaitGroup
		var mu sync.Mutex
		for _, task := range ready {
			task := task
			delete(pending, task.Name)
			wg.Add(1)
			go func() {
				defer wg.Done()
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-ctx.Done():
					mu.Lock()
					results[task.Name] = TaskResult{Name: task.Name, State: "cancelled", ExitCode: 1}
					mu.Unlock()
					return
				}
				args := []string{}
				if rootTasks[task.Name] {
					args = options.Args
				}
				r := runWorkspaceTask(ctx, *w, executable, task, options, args, emit)
				mu.Lock()
				results[task.Name] = r
				if r.ExitCode != 0 && failedCode == 0 {
					failedCode = r.ExitCode
					cancel()
				}
				mu.Unlock()
			}()
		}
		wg.Wait()
		if failedCode != 0 {
			break
		}
	}
	for _, task := range plan.Tasks {
		r, ok := results[task.Name]
		if !ok {
			r = TaskResult{Name: task.Name, State: "skipped", ExitCode: 1}
		}
		report.Tasks = append(report.Tasks, r)
	}
	if parent.Err() != nil {
		return report, parent.Err()
	}
	if failedCode != 0 {
		return report, TaskRunError{failedCode, "项目任务执行失败"}
	}
	if len(pending) > 0 {
		return report, fmt.Errorf("任务依赖未完成")
	}
	report.Success = true
	return report, nil
}

type taskOutput struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	truncated bool
	name      string
	emit      func(TaskEvent)
}

func (w *taskOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	remaining := (64 << 10) - w.buf.Len()
	if len(p) > remaining {
		w.truncated = true
	}
	w.buf.Write(p[:min(len(p), remaining)])
	if w.emit != nil {
		for len(p) > 0 {
			part := p[:min(len(p), 8192)]
			w.emit(TaskEvent{Task: w.name, State: "output", Output: strings.ToValidUTF8(string(part), "�")})
			p = p[len(part):]
		}
	}
	return n, nil
}

func runWorkspaceTask(ctx context.Context, w Workspace, executable string, task PlannedTask, options TaskOptions, args []string, emit func(TaskEvent)) TaskResult {
	start := time.Now()
	result := TaskResult{Name: task.Name, State: "running"}
	emit(TaskEvent{Task: task.Name, State: "running"})
	finish := func(err error) TaskResult {
		result.DurationMS = time.Since(start).Milliseconds()
		if err != nil {
			result.State = "failed"
			result.ExitCode = 1
			result.Error = err.Error()
			if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() > 0 {
				result.ExitCode = ee.ExitCode()
			}
		} else if result.State != "cached" {
			result.State = "success"
		}
		level := "info"
		if result.ExitCode != 0 {
			level = "error"
		}
		logging.Record(level, "tasks", "task."+result.State, logging.Fields{"task": task.Name, "exit_code": result.ExitCode, "duration_ms": result.DurationMS})
		emit(TaskEvent{Task: task.Name, State: result.State})
		return result
	}
	if !w.Trusted() {
		return finish(fmt.Errorf("任务启动前配置已变更，请重新检查并信任"))
	}
	def := task.Definition
	if len(def.Command) == 0 {
		return finish(nil)
	}
	workDir := w.Root
	if def.Dir != "" {
		workDir = filepath.Join(w.Root, filepath.FromSlash(def.Dir))
		resolved, err := canonicalPath(workDir)
		if err != nil || !pathWithin(w.Root, resolved) {
			return finish(fmt.Errorf("task working directory escapes project"))
		}
		workDir = resolved
	}
	cacheKey := ""
	var err error
	project, err := findProjectBounded(workDir, w.Root)
	if err != nil {
		return finish(err)
	}
	// Docker tags can be retargeted without a project-file change. Until the
	// cache records the exact image used by execution, always run these tasks.
	cacheable := project == nil || project.Mode != "docker"
	if cacheable && !options.NoCache && len(def.Inputs) > 0 && len(def.Outputs) > 0 && len(args) == 0 {
		cacheKey, err = taskFingerprint(w, task, options, workDir)
		if err != nil {
			return finish(err)
		}
		if taskCacheHit(w, task, cacheKey) {
			result.State = "cached"
			return finish(nil)
		}
	}
	timeout := def.TimeoutSeconds
	if timeout == 0 {
		timeout = 600
	}
	jobCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cliArgs := []string{"run"}
	for k, v := range def.Env {
		cliArgs = append(cliArgs, "--env", k+"="+v)
	}
	if options.Locked {
		cliArgs = append(cliArgs, "--locked")
	}
	cliArgs = append(cliArgs, "--")
	cliArgs = append(cliArgs, def.Command...)
	cliArgs = append(cliArgs, args...)
	cmd := exec.CommandContext(jobCtx, executable, cliArgs...)
	cmd.Dir = workDir
	cmd.Env = os.Environ()
	cmd.Env = replaceEnv(cmd.Env, "VSM_PROJECT_BOUNDARY", w.Root)
	if options.RequestID != "" {
		cmd.Env = replaceEnv(cmd.Env, "VSM_MCP_REQUEST_ID", options.RequestID)
	}
	for k, v := range def.Env {
		cmd.Env = replaceEnv(cmd.Env, k, v)
	}
	mcpserver.ConfigureProcess(cmd)
	cmd.WaitDelay = 2 * time.Second
	output := &taskOutput{name: task.Name, emit: emit}
	cmd.Stdout = output
	cmd.Stderr = output
	err = cmd.Run()
	result.Output = strings.ToValidUTF8(output.buf.String(), "�")
	result.Truncated = output.truncated
	if jobCtx.Err() != nil {
		return finish(jobCtx.Err())
	}
	if err == nil && cacheKey != "" {
		if cacheErr := writeTaskCache(w, task, cacheKey); cacheErr != nil {
			result.Error = "cache: " + cacheErr.Error()
		}
	}
	return finish(err)
}

func replaceEnv(env []string, key, value string) []string {
	out := make([]string, 0, len(env)+1)
	for _, pair := range env {
		k, _, _ := strings.Cut(pair, "=")
		if k == key || (runtime.GOOS == "windows" && strings.EqualFold(k, key)) {
			continue
		}
		out = append(out, pair)
	}
	return append(out, key+"="+value)
}

func hashWorkspaceFiles(root string, patterns []string) (string, error) {
	h := sha256.New()
	names := []string{}
	var total int64
	guard, err := os.OpenRoot(root)
	if err != nil {
		return "", err
	}
	defer guard.Close()
	for _, pattern := range patterns {
		matches, err := workspaceGlob(guard.FS(), pattern)
		if err != nil {
			return "", err
		}
		if len(matches) == 0 {
			return "", fmt.Errorf("cache pattern matched no files: %s", pattern)
		}
		for _, name := range matches {
			fi, err := fs.Stat(guard.FS(), name)
			if err != nil {
				return "", err
			}
			if !fi.Mode().IsRegular() {
				return "", fmt.Errorf("cache patterns must select regular files: %s", name)
			}
			names = append(names, name)
		}
	}
	if len(names) > 10000 {
		return "", fmt.Errorf("cache file limit exceeded")
	}
	sort.Strings(names)
	for _, name := range names {
		f, err := guard.Open(filepath.FromSlash(name))
		if err != nil {
			return "", err
		}
		fileHash := sha256.New()
		n, err := io.Copy(fileHash, io.LimitReader(f, (512<<20)+1))
		f.Close()
		total += n
		if err != nil {
			return "", err
		}
		if total > 512<<20 {
			return "", fmt.Errorf("cache input size limit exceeded")
		}
		_ = binary.Write(h, binary.BigEndian, uint64(len(name)))
		h.Write([]byte(name))
		h.Write(fileHash.Sum(nil))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func workspaceGlob(root fs.FS, pattern string) ([]string, error) {
	if !strings.Contains(pattern, "**") {
		return fs.Glob(root, pattern)
	}
	for _, part := range strings.Split(pattern, "/") {
		if part != "**" {
			if _, err := filepath.Match(part, ""); err != nil {
				return nil, err
			}
		}
	}
	parts := strings.Split(pattern, "/")
	out := []string{}
	visited := 0
	err := fs.WalkDir(root, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		visited++
		if visited > 30000 {
			return fmt.Errorf("recursive cache scan limit exceeded")
		}
		if entry.IsDir() {
			if name != "." {
				switch entry.Name() {
				case ".git", ".vsm", "node_modules", ".venv":
					return fs.SkipDir
				}
			}
			return nil
		}
		segments := strings.Split(name, "/")
		memo := map[[2]int]bool{}
		seen := map[[2]int]bool{}
		var match func(int, int) bool
		match = func(i, j int) bool {
			key := [2]int{i, j}
			if seen[key] {
				return memo[key]
			}
			seen[key] = true
			result := false
			if i == len(parts) {
				result = j == len(segments)
			} else if parts[i] == "**" {
				result = match(i+1, j) || (j < len(segments) && match(i, j+1))
			} else if j < len(segments) {
				ok, _ := filepath.Match(parts[i], segments[j])
				result = ok && match(i+1, j+1)
			}
			memo[key] = result
			return result
		}
		if match(0, 0) {
			out = append(out, name)
		}
		return nil
	})
	return out, err
}

func taskFingerprint(w Workspace, task PlannedTask, options TaskOptions, workDir string) (string, error) {
	inputs, err := hashWorkspaceFiles(w.Root, task.Definition.Inputs)
	if err != nil {
		return "", err
	}
	env, err := w.Environment()
	if err != nil {
		return "", err
	}
	// Inherited variables can change build outputs. Hash values without storing them.
	baseEnv := os.Environ()
	sort.Strings(baseEnv)
	toolchain, err := taskToolchain(workDir, w.Root, options.Locked)
	if err != nil {
		return "", err
	}
	data, _ := json.Marshal([]any{task, inputs, env, baseEnv, toolchain, options.Locked, runtime.GOOS, runtime.GOARCH, buildinfo.Version})
	h := sha256.New()
	h.Write(data)
	for _, file := range []string{config.WorkspaceName, LockName, ".tool-versions"} {
		if data, err := config.ReadBounded(filepath.Join(w.Root, file), 4<<20); err == nil {
			h.Write(data)
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func taskPaths(global Paths, dir, boundary string) (Paths, error) {
	project, err := findProjectBounded(dir, boundary)
	if err != nil {
		return Paths{}, err
	}
	if project == nil {
		return global, nil
	}
	if err := project.ValidateStateRoot(); err != nil {
		return Paths{}, err
	}
	return project.Paths(global), nil
}

// Record the resolved tools as well as the declarations: a broad requirement
// can select a different build after installation without any config change.
func taskToolchain(dir, boundary string, locked bool) (any, error) {
	global, err := DefaultPaths()
	if err != nil {
		return nil, err
	}
	paths, err := taskPaths(global, dir, boundary)
	if err != nil {
		return nil, err
	}
	registry := Registry{Paths: paths}
	resolver := Resolver{Paths: paths, Registry: registry, Boundary: boundary}
	if locked {
		lock, err := LoadLock(dir)
		if err != nil {
			return nil, err
		}
		if err := lock.validateRequirementsBounded(dir, boundary); err != nil {
			return nil, err
		}
		resolver.Pinned = map[string]string{}
		for name, entry := range lock.Tools {
			resolver.Pinned[name] = entry.Version
		}
	}
	plan, err := (Activator{Paths: paths, Resolver: resolver, Registry: registry}).PlanProjectEnvironment(dir)
	if err != nil {
		return nil, err
	}
	type identity struct {
		Tool, Version, Directory, Definition string
		Receipt                              []byte
	}
	tools := []identity{}
	for _, resolved := range plan.Versions {
		p, _ := plugin.Get(resolved.Tool)
		directory, err := canonicalPath(paths.InstallDir(resolved.Tool, resolved.Version))
		if err != nil {
			return nil, err
		}
		receipt, err := config.ReadBounded(filepath.Join(directory, artifactReceiptName), 16<<10)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		tools = append(tools, identity{resolved.Tool, resolved.Version, directory, plugin.DefinitionID(p), receipt})
	}
	return []any{plan, tools}, nil
}

func taskCachePath(w Workspace, task PlannedTask) string {
	sum := sha256.Sum256([]byte(task.Name))
	return filepath.Join(w.Root, ".vsm", "task-cache", hex.EncodeToString(sum[:])+".json")
}

type taskCache struct {
	Key     string `json:"key"`
	Outputs string `json:"outputs"`
}

func taskCacheHit(w Workspace, task PlannedTask, key string) bool {
	data, err := config.ReadBounded(taskCachePath(w, task), 4096)
	if err != nil {
		return false
	}
	var cache taskCache
	if config.DecodeStrict(data, &cache) != nil || cache.Key != key {
		return false
	}
	outputs, err := hashWorkspaceFiles(w.Root, task.Definition.Outputs)
	return err == nil && outputs == cache.Outputs
}
func writeTaskCache(w Workspace, task PlannedTask, key string) error {
	outputs, err := hashWorkspaceFiles(w.Root, task.Definition.Outputs)
	if err != nil {
		return err
	}
	data, _ := json.Marshal(taskCache{key, outputs})
	root, err := os.OpenRoot(w.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	rel, err := filepath.Rel(w.Root, taskCachePath(w, task))
	if err != nil {
		return err
	}
	return config.WriteRootAtomic(root, rel, data, 0o600)
}
