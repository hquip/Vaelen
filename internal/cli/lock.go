package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"vsm/internal/core"
	"vsm/internal/plugin"
)

func (a *app) cmdLock(args []string) error {
	update, check, asJSON := false, false, false
	targets := []plugin.Target{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--update":
			update = true
		case "--check":
			check = true
		case "--json":
			asJSON = true
		case "--platform":
			if i+1 >= len(args) {
				return fmt.Errorf("--platform requires os/arch")
			}
			i++
			for _, raw := range strings.Split(args[i], ",") {
				target, err := core.ParseTarget(raw)
				if err != nil {
					return err
				}
				targets = append(targets, target)
			}
		default:
			return fmt.Errorf("用法: vsm lock [--platform os/arch,...] [--update|--check] [--json]")
		}
	}
	if check && update {
		return fmt.Errorf("--check cannot be combined with --update")
	}
	if check {
		lock, err := core.LoadLock(a.cwd)
		if err != nil {
			return err
		}
		if err := lock.ValidateRequirements(a.cwd); err != nil {
			return err
		}
		if asJSON {
			return printJSON(lock)
		}
		fmt.Println("vsm.lock 与项目要求一致")
		return nil
	}
	if a.paths.ProjectMode == "docker" {
		return fmt.Errorf("Docker 项目由镜像提供工具；工具包锁定请使用 local/versions 项目")
	}
	lock, err := core.CreateLock(a.cwd, targets, update, func(s string) { fmt.Fprintln(os.Stderr, "[vsm]", s) })
	if err != nil {
		return err
	}
	if asJSON {
		return printJSON(lock)
	}
	fmt.Printf("已锁定 %d 个工具到 vsm.lock\n", len(lock.Tools))
	return nil
}

func (a *app) useLock(install bool) error {
	if a.paths.ProjectMode == "docker" {
		return fmt.Errorf("--locked 工具包安装适用于 local/versions；Docker 请固定镜像摘要")
	}
	lock, err := core.LoadLock(a.cwd)
	if err != nil {
		return err
	}
	if err := lock.ValidateRequirements(a.cwd); err != nil {
		return err
	}
	key := plugin.CurrentTarget().OS + "/" + plugin.CurrentTarget().Arch
	names := []string{}
	for name := range lock.Tools {
		names = append(names, name)
	}
	sort.Strings(names)
	pins := map[string]string{}
	for _, name := range names {
		entry := lock.Tools[name]
		artifact, ok := entry.Platforms[key]
		if !ok {
			return fmt.Errorf("vsm.lock 缺少 %s/%s；运行 vsm lock --platform %s", name, key, key)
		}
		if install {
			p, _ := plugin.Get(name)
			in := a.inst
			in.Pinned = &artifact
			in.RequireChecksum = true
			if err := in.Install(p, entry.Version, "", func(s string) { fmt.Fprintln(os.Stderr, "[vsm]", s) }); err != nil {
				return err
			}
		} else {
			if err := core.CheckInstalledArtifact(a.paths.InstallDir(name, entry.Version), artifact); err != nil {
				return fmt.Errorf("%s: %w；使用 vsm install --locked", name, err)
			}
		}
		pins[name] = entry.Version
	}
	a.res.Pinned = pins
	a.act.Resolver = a.res
	if install {
		return a.act.GenerateShims()
	}
	return nil
}
