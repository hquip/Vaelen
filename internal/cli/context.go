package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"vsm/internal/buildinfo"
	"vsm/internal/core"
	"vsm/internal/plugin"
)

type suggestedCommand struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Reason  string   `json:"reason"`
}

type contextTool struct {
	Tool             string             `json:"tool"`
	Requested        string             `json:"requested_version"`
	Resolved         string             `json:"resolved_version"`
	Active           string             `json:"active_version"`
	Source           string             `json:"source"`
	ProjectRequired  string             `json:"project_requirement"`
	ProjectSource    string             `json:"project_source"`
	Executable       string             `json:"executable,omitempty"`
	State            string             `json:"state"`
	OverrideConflict bool               `json:"override_conflict"`
	Suggestions      []suggestedCommand `json:"suggestions"`
	Error            string             `json:"error,omitempty"`
}

func (a *app) cmdContext(args []string) error {
	rest, asJSON := jsonFlag(args)
	check := false
	for _, arg := range rest {
		if arg != "--check" {
			return fmt.Errorf("用法: vsm context [--json] [--check]")
		}
		check = true
	}
	active := map[string]string{}
	for _, pair := range strings.Split(os.Getenv("_VSM_ACTIVE_VERSIONS"), ",") {
		k, v, ok := strings.Cut(pair, "@")
		if ok {
			active[k] = v
		}
	}
	items := []contextTool{}
	ready := true
	for _, tool := range plugin.Names() {
		r := a.res.Resolve(tool, a.cwd)
		proj, src, err := core.ProjectRequirement(tool, a.cwd)
		if r.Spec == "" && r.Error == "" && src == "" && err == nil {
			continue
		}
		item := contextTool{Tool: tool, Requested: r.Spec, Resolved: r.Version, Active: active[tool], Source: r.Source, ProjectRequired: proj, ProjectSource: src, State: "ready", Suggestions: []suggestedCommand{}, Error: r.Error}
		if err != nil {
			item.Error = err.Error()
		}
		if item.Error != "" {
			item.State = "error"
		} else if r.Version == "" {
			item.State = "missing_version"
			item.Suggestions = append(item.Suggestions, suggestedCommand{"vsm", []string{"install", tool, r.Spec, "--require-checksum"}, "安装项目要求的版本，并要求发布方校验值"})
		} else {
			expected, whichErr := a.act.Which(tool, a.cwd)
			actual, lookErr := exec.LookPath(tool)
			if lookErr != nil {
				for _, name := range plugin.DetectCommands()[tool] {
					actual, lookErr = exec.LookPath(name)
					if lookErr == nil {
						break
					}
				}
			}
			if lookErr == nil {
				item.Executable = actual
			}
			matches := false
			if whichErr == nil && lookErr == nil {
				e, eErr := filepath.EvalSymlinks(expected)
				v, vErr := filepath.EvalSymlinks(actual)
				matches = eErr == nil && vErr == nil && e == v
			}
			if item.Active != r.Version || !matches {
				item.State = "activation_required"
				item.Suggestions = append(item.Suggestions, suggestedCommand{"vsm", []string{"run", "--", tool, "<args>"}, "自动选择项目声明的所有工具版本并执行，无需修改父 shell"})
				item.Suggestions = append(item.Suggestions, suggestedCommand{"vsm", []string{"shell", tool, r.Spec}, "在临时子 shell 中使用该版本，退出后恢复"}, suggestedCommand{"vsm", []string{"exec", tool, "--", "<command>"}, "让 Agent 命令明确使用解析出的版本"})
			}
		}
		if proj != "" && r.Version != "" && core.BestMatch(proj, []string{r.Version}) == "" {
			item.OverrideConflict = true
			item.State = "override_conflict"
			item.Suggestions = append(item.Suggestions, suggestedCommand{"vsm", []string{"shell", tool, proj}, "当前环境覆盖与项目要求不同，显式选择项目版本"})
			item.Suggestions = append(item.Suggestions, suggestedCommand{"vsm", []string{"run", "--", tool, "<args>"}, "按项目声明执行，覆盖继承的旧版本设置"})
		}
		if item.State != "ready" {
			ready = false
		}
		items = append(items, item)
	}
	status := "ready"
	if !ready {
		status = "attention_required"
	}
	if asJSON {
		if err := printJSON(map[string]any{"schema_version": 1, "vsm_version": buildinfo.Version, "directory": a.cwd, "status": status, "tools": items, "read_only": true}); err != nil {
			return err
		}
	} else {
		fmt.Printf("项目: %s\n状态: %s\n", a.cwd, status)
		for _, item := range items {
			fmt.Printf("%s: 项目要求=%s 解析=%s 已激活=%s [%s]\n", item.Tool, item.ProjectRequired, item.Resolved, item.Active, item.State)
			if item.Error != "" {
				fmt.Println("  " + item.Error)
			}
			for _, s := range item.Suggestions {
				fmt.Printf("  %s %s — %s\n", s.Command, strings.Join(s.Args, " "), s.Reason)
			}
		}
	}
	if check && !ready {
		return fmt.Errorf("版本环境尚未满足项目要求，参见 context 输出")
	}
	return nil
}
