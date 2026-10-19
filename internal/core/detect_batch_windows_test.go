package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBatchVersionProbeHandlesSpacesAndMetacharacters(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Maven & bang!")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "mvn.cmd")
	if err := os.WriteFile(bin, []byte("@echo off\r\necho Apache Maven 3.9.8\r\n"), 0644); err != nil {
		t.Fatal(err)
	}
	version, err := probeVersion("maven", bin)
	if err != nil || version != "3.9.8" {
		t.Fatalf("%s %v", version, err)
	}
}
