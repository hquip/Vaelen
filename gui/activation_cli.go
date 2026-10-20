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
func (a *App) activationComponents() (*components, error) {
	c := a.c()
	if checkActivationCLI(c.act.VsmExe) == nil {
		return c, nil
	}
	path := locateVsmCLI()
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
			client := updater.New().WithProgress(func(p updater.Progress) {
				if a.ctx != nil {
					wruntime.EventsEmit(a.ctx, "path:progress", p)
				}
			})
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
