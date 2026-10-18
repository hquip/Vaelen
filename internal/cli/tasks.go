package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"vsm/internal/core"
)

func (a *app) cmdTasks(args []string) error {
	operation := "list"
	if len(args) > 0 {
		operation = args[0]
		args = args[1:]
	}
	if operation == "list" || operation == "graph" {
		rest, asJSON := jsonFlag(args)
		plan, err := core.PlanTasks(a.cwd, rest)
		if err != nil {
			return err
		}
		if asJSON {
			return printJSON(plan)
		}
		for _, task := range plan.Tasks {
			fmt.Printf("%-18s %s", task.Name, task.Definition.Description)
			if len(task.Definition.Depends) > 0 {
				fmt.Printf("  ← %s", strings.Join(task.Definition.Depends, ", "))
			}
			fmt.Println()
		}
		return nil
	}
	if operation != "run" {
		return fmt.Errorf("用法: vsm tasks list|graph|run <任务> [--jobs N] [--locked] [--no-cache] [--json] [-- 参数]")
	}
	options := core.TaskOptions{}
	names := []string{}
	asJSON, dry := false, false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--":
			options.Args = args[i+1:]
			i = len(args)
		case "--json":
			asJSON = true
		case "--dry-run":
			dry = true
		case "--locked":
			options.Locked = true
		case "--no-cache":
			options.NoCache = true
		case "--jobs":
			if i+1 >= len(args) {
				return fmt.Errorf("--jobs requires 1–16")
			}
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil {
				return err
			}
			if n < 1 || n > 16 {
				return fmt.Errorf("jobs must be 1..16")
			}
			options.Jobs = n
		default:
			if strings.HasPrefix(args[i], "-") {
				return fmt.Errorf("unknown task option %s", args[i])
			}
			names = append(names, args[i])
		}
	}
	if len(names) == 0 {
		return fmt.Errorf("至少指定一个任务")
	}
	if dry {
		plan, err := core.PlanTasks(a.cwd, names)
		if err != nil {
			return err
		}
		return printJSON(plan)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if !asJSON {
		options.OnEvent = func(event core.TaskEvent) {
			if event.Output != "" {
				fmt.Fprintf(os.Stderr, "[%s] %s", event.Task, event.Output)
			} else {
				fmt.Fprintf(os.Stderr, "[%s] %s\n", event.Task, event.State)
			}
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	report, runErr := core.RunTasks(ctx, a.cwd, exe, names, options)
	if asJSON {
		if err := printJSON(report); err != nil {
			return err
		}
	}
	return runErr
}
