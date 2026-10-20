package main

import (
	"os"
	"path/filepath"
	"testing"
	"vsm/internal/netcfg"
	"vsm/internal/plugin"
)

func TestProjectGUIWritesEffectiveGlobalNetworkSettings(t *testing.T) {
	a := newTestApp(t)
	dir := t.TempDir()
	t.Setenv("VSM_MIRROR", "")
	t.Setenv("VSM_PROXY", "")
	if err := a.SetProjectDir(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := a.InitProjectEnvironment("local", ""); err != nil {
		t.Fatal(err)
	}
	if err := a.SetProxy("socks5h://127.0.0.1:1080"); err != nil {
		t.Fatal(err)
	}
	if netcfg.ProxySetting() != "socks5h://127.0.0.1:1080" {
		t.Fatal("GUI proxy does not reach transport")
	}
	if err := a.SetMirror("custom"); err != nil {
		t.Fatal(err)
	}
	if plugin.MirrorMode() != "custom" {
		t.Fatal("GUI mirror does not reach transport")
	}
	if err := a.SetMirror("auto"); err != nil || a.GetMirror() != "auto" {
		t.Fatalf("automatic mode was not persisted: %s %v", a.GetMirror(), err)
	}
	for _, name := range []string{"proxy", "mirror"} {
		if _, err := os.Stat(filepath.Join(dir, ".vsm", "env", name)); !os.IsNotExist(err) {
			t.Fatal("network settings stored in unused project location")
		}
	}
}
