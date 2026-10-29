package main

import (
	"runtime"

	"vsm/internal/core"
	"vsm/internal/logging"
)

// EnsureCommand runs after the UI has subscribed to component-download events.
// Always register the global command, even when the GUI opens a local/docker
// project. Full language activation remains a separate PathInit operation.
func (a *App) EnsureCommand() (result core.CommandSetup, err error) {
	operationID := logging.NewOperationID()
	defer logGUI("command.setup", logging.Fields{"operation_id": operationID}, &err)()
	if runtime.GOOS != "windows" {
		return result, nil
	}
	if !a.mu.TryLock() {
		return result, a.busyError()
	}
	defer a.mu.Unlock()
	paths, err := core.DefaultPaths()
	if err != nil {
		return result, err
	}
	c, err := a.activationComponents()
	if err != nil {
		return result, err
	}
	act := core.Activator{Paths: paths, VsmExe: c.act.VsmExe, TraceID: operationID}
	if a.commandRegistrar != nil {
		return a.commandRegistrar(act)
	}
	return act.RegisterWindowsCommand()
}
