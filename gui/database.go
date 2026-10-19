package main

import (
	"context"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"vsm/internal/core"
	"vsm/internal/database"
)

func (a *App) DatabaseEngines() []database.Engine { return database.Engines }
func (a *App) databaseManager() (*database.Manager, error) {
	paths, err := core.DefaultPaths()
	if err != nil {
		return nil, err
	}
	m := database.New(paths)
	m.Progress = func(text string) {
		if a.ctx != nil {
			wruntime.EventsEmit(a.ctx, "database:progress", text)
		}
	}
	return m, nil
}
func (a *App) DatabaseList() ([]database.Status, error) {
	m, err := a.databaseManager()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return m.List(ctx)
}
func (a *App) DatabaseNativeVersions(engine string) ([]string, error) {
	m, err := a.databaseManager()
	if err != nil {
		return nil, err
	}
	return m.NativeVersions(engine)
}
func (a *App) databaseAction(action func(context.Context, *database.Manager) (database.Status, error)) (database.Status, error) {
	if !a.mu.TryLock() {
		return database.Status{}, errBusy
	}
	defer a.mu.Unlock()
	m, err := a.databaseManager()
	if err != nil {
		return database.Status{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	a.workMu.Lock()
	a.databaseCancel = cancel
	a.workMu.Unlock()
	defer func() { a.workMu.Lock(); a.databaseCancel = nil; a.workMu.Unlock() }()
	return action(ctx, m)
}
func (a *App) DatabaseUse(request database.Request) (database.Status, error) {
	return a.databaseAction(func(ctx context.Context, m *database.Manager) (database.Status, error) { return m.Use(ctx, request) })
}
func (a *App) DatabaseStart(name string) (database.Status, error) {
	return a.databaseAction(func(ctx context.Context, m *database.Manager) (database.Status, error) { return m.Start(ctx, name) })
}
func (a *App) DatabaseStop(name string) (database.Status, error) {
	return a.databaseAction(func(ctx context.Context, m *database.Manager) (database.Status, error) { return m.Stop(ctx, name) })
}
func (a *App) CancelDatabaseOperation() bool {
	a.workMu.Lock()
	defer a.workMu.Unlock()
	if a.databaseCancel == nil {
		return false
	}
	a.databaseCancel()
	return true
}
func (a *App) DatabaseLogs(name string) (string, error) {
	m, err := a.databaseManager()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return m.Logs(ctx, name)
}
func (a *App) DatabaseConnection(name string) (database.Connection, error) {
	m, err := a.databaseManager()
	if err != nil {
		return database.Connection{}, err
	}
	return m.Connection(name)
}
func (a *App) DatabaseAdoptNative(engine, path string) ([]string, error) {
	if !a.mu.TryLock() {
		return nil, errBusy
	}
	defer a.mu.Unlock()
	paths, err := core.DefaultPaths()
	if err != nil {
		return nil, err
	}
	if _, err := database.New(paths).NativeVersions(engine); err != nil {
		return nil, err
	}
	if _, err := (core.Installer{Paths: paths}).AdoptPath(engine, path, ""); err != nil {
		return nil, err
	}
	return database.New(paths).NativeVersions(engine)
}
