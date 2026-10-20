package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"vsm/internal/buildinfo"
	"vsm/internal/logging"
	"vsm/internal/updater"
)

// Called under the GUI write lock. Discovery is rechecked because the program
// may have been moved or its matching CLI downloaded since the GUI started.
func (a *App) activationComponents() (result *components, err error) {
	c := a.c()
	if checkActivationCLI(c.act.VsmExe) == nil {
		return c, nil
	}
	path := locateVsmCLI()
	preparing := path == ""
	if preparing {
		a.cliPreparing.Store(true)
		a.emitCLIProgress(updater.Progress{Stage: "checking"})
		finish := logging.Start("gui", "cli.prepare", logging.Fields{"version": buildinfo.DisplayVersion()})
		defer func() {
			a.cliPreparing.Store(false)
			stage := "failed"
			if err == nil {
				stage = "ready"
			}
			a.emitCLIProgress(updater.Progress{Stage: stage})
			finish(err)
		}()
	}
	if path == "" {
		executable, err := os.Executable()
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		directory := filepath.Dir(executable)
		if a.cliInstaller != nil {
			path, err = a.cliInstaller(ctx, buildinfo.DisplayVersion(), directory)
		} else {
			client := updater.New().WithProgress(a.emitCLIProgress)
			path, err = client.InstallCLI(ctx, buildinfo.DisplayVersion(), directory)
		}
		if err != nil {
			return nil, fmt.Errorf("没有找到与 GUI %s 匹配的 CLI，自动准备失败: %w", buildinfo.DisplayVersion(), err)
		}
	}
	if err := checkActivationCLI(path); err != nil {
		return nil, err
	}
	updated := *c
	updated.act.VsmExe = path
	a.st.Store(&updated)
	logging.Record("info", "gui", "cli.selected", logging.Fields{"path": path, "version": buildinfo.DisplayVersion()})
	return &updated, nil
}

func (a *App) emitCLIProgress(progress updater.Progress) {
	if a.cliProgress != nil {
		a.cliProgress(progress)
	} else if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "path:progress", progress)
	}
}
