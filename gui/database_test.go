package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"vsm/internal/database"
)

func TestDatabaseGUIBusyAndReadOnlyListing(t *testing.T) {
	a := newTestApp(t)
	list, err := a.DatabaseList()
	if err != nil || len(list) != 0 {
		t.Fatalf("%+v %v", list, err)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("VSM_HOME"), "databases")); !os.IsNotExist(err) {
		t.Fatal("listing created database state")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, err := a.DatabaseUse(database.Request{Engine: "postgres", Version: "17"}); !errors.Is(err, errBusy) {
		t.Fatal(err)
	}
	if _, err := a.DatabaseStart("dev"); !errors.Is(err, errBusy) {
		t.Fatal(err)
	}
	if _, err := a.DatabaseStop("dev"); !errors.Is(err, errBusy) {
		t.Fatal(err)
	}
}
func TestDatabaseAdoptionRejectsNonDatabaseTools(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.DatabaseAdoptNative("node", t.TempDir()); err == nil {
		t.Fatal("database entry point adopted a different runtime")
	}
}
