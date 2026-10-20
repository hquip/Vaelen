package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"vsm/internal/config"
	"vsm/internal/logging"
	"vsm/internal/plugin"
)

// LogActivationSnapshot inspects only routing metadata. It never runs language
// programs, evaluates AutoRun, or records arbitrary command arguments/output.
func (a Activator) LogActivationSnapshot(scope, phase, mode, dir string, tools []string) {
	if logging.Level() == "off" {
		return
	}
	if a.TraceID == "" {
		a.TraceID = logging.NewOperationID()
	}
	paths := filepath.SplitList(os.Getenv("PATH"))
	shimIndex := -1
	for i, path := range paths {
		if sameDir(path, a.Paths.Shims()) {
			shimIndex = i
			break
		}
	}
	fields := a.activationFields()
	fields["scope"], fields["phase"], fields["cmd_mode"] = scope, phase, mode
	fields["directory"], fields["path_entries"] = dir, len(paths)
	fields["shims_index"] = shimIndex
	fields["path_head"] = paths[:min(len(paths), 12)]
	fields["pathext"] = os.Getenv("PATHEXT")
	fields["configured_home"] = os.Getenv("VSM_HOME")
	fields["helper_session"] = os.Getenv("_VSM_CMD_TRACE_ID")
	fields["helper_scope"] = os.Getenv("_VSM_CMD_SCOPE")
	fields["parent_pid"] = os.Getppid()
	level := "info"
	if shimIndex != 0 {
		level = "warn"
	}
	logging.Record(level, "activation", "environment.snapshot", fields)
	global, err := config.Load(a.Paths.GlobalToolVersions())
	if err != nil && !os.IsNotExist(err) {
		fields := a.activationFields()
		fields["phase"], fields["error"] = phase, logging.ErrorMessage(err)
		logging.Record("warn", "activation", "config.read.failed", fields)
	}
	if len(tools) == 0 && global != nil {
		tools = global.Tools()
	}
	for _, tool := range tools[:min(len(tools), 16)] {
		if _, ok := plugin.Get(tool); !ok || plugin.IsServiceTool(tool) {
			continue
		}
		r := a.Resolver.Resolve(tool, dir)
		names := plugin.DetectCommands()[tool]
		if len(names) == 0 {
			names = []string{tool}
		}
		fields := a.activationFields()
		fields["scope"], fields["phase"], fields["cmd_mode"] = scope, phase, mode
		fields["helper_session"] = os.Getenv("_VSM_CMD_TRACE_ID")
		fields["tool"], fields["effective_version"], fields["source"] = tool, r.Version, r.Source
		if global != nil {
			fields["global_version"], _ = global.Get(tool)
		}
		key := "VSM_" + strings.ToUpper(tool) + "_VERSION"
		fields["override_key"], fields["override_version"] = key, os.Getenv(key)
		limit := 1
		if phase == "manual" || phase == "before-log-export" {
			limit = 16
		}
		fields["candidate_limit"] = limit
		candidates := activationCandidates(dir, paths, names[0], limit)
		fields["candidates"] = candidates
		if len(candidates) > 0 {
			fields["command"] = candidates[0]
		}
		if executable, err := a.Which(tool, dir); err == nil {
			fields["selected_executable"] = executable
		} else {
			fields["error"] = logging.ErrorMessage(err)
		}
		if tool == "java" {
			fields["java_home"] = os.Getenv("JAVA_HOME")
		} else if tool == "go" {
			fields["goroot"] = os.Getenv("GOROOT")
		}
		logging.Record("info", "activation", "command.route", fields)
	}
}

func (a Activator) activationFields() logging.Fields {
	return logging.Fields{"operation_id": a.TraceID, "data_dir": a.Paths.Root,
		"installs_dir": a.Paths.Installs(), "global_config": a.Paths.GlobalToolVersions(), "shims_dir": a.Paths.Shims(), "cli": a.VsmExe}
}

func activationCandidates(dir string, paths []string, name string, limit int) []string {
	dirs := paths
	if runtime.GOOS == "windows" {
		dirs = append([]string{dir}, paths...)
	}
	var candidates []string
	seen := map[string]bool{}
	for _, path := range dirs {
		for _, ext := range executableExts() {
			file := filepath.Join(path, name+ext)
			key := normDir(file)
			if !seen[key] && fileExists(file) {
				seen[key] = true
				candidates = append(candidates, file)
				if len(candidates) == limit {
					return candidates
				}
			}
		}
	}
	return candidates
}
