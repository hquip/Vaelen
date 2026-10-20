package main

import (
	"fmt"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"vsm/internal/logging"
)

// Read-only refreshes only produce a record when they fail.
func logGUIError(event string, err *error) func() {
	return func() {
		if *err != nil {
			logging.Record("error", "gui", event, logging.Fields{"error": (*err).Error()})
		}
	}
}

func logGUI(event string, fields logging.Fields, err *error) func() {
	finish := logging.Start("gui", event, fields)
	return func() { finish(*err) }
}

// Log operations bypass project configuration so users can diagnose a broken
// project without first fixing or deselecting it.
func (a *App) Logs(tail int, level string) (logging.Snapshot, error) {
	store, err := logging.Default()
	if err != nil {
		return logging.Snapshot{}, err
	}
	return store.Read(tail, level)
}

func (a *App) ClearLogs() error {
	store, err := logging.Default()
	if err != nil {
		return err
	}
	return store.Clear()
}

func (a *App) ExportLogs() (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("界面未就绪")
	}
	path, err := wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{
		Title:           "导出 VSM 诊断日志（请选择新文件）",
		DefaultFilename: "vsm-logs-" + time.Now().Format("20060102-150405") + ".jsonl",
		Filters:         []wruntime.FileFilter{{DisplayName: "JSON Lines 日志", Pattern: "*.jsonl"}},
	})
	if err != nil || path == "" {
		return "", err
	}
	a.c().act.LogActivationSnapshot("gui-process", "before-log-export", "", a.resolveDir(), nil)
	store, err := logging.Default()
	if err != nil {
		return "", err
	}
	if err := store.Export(path); err != nil {
		return "", err
	}
	return path, nil
}
