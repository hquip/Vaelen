package core

import (
	"os"
	"path/filepath"
	"testing"

	"vsm/internal/config"
)

func TestCmdMetadataVerificationDoesNotStartLicensedHostAndRejectsTampering(t *testing.T) {
	a, _ := executingActivationFixture(t)
	root := a.Paths.InstallDir("matlab", "25.1.0.12345")
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "matlab.exe"), []byte("MUST_NOT_START_OR_CHECK_OUT_LICENSE"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "VersionInfo.xml"), []byte(`<MathWorks_version_info><version>25.1.0.12345</version></MathWorks_version_info>`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.SetGlobalVersion("matlab", "25.1.0.12345"); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteFileAtomic(filepath.Join(a.Paths.Root, "vsm-cmd-init.cmd"), []byte(cmdStartupScript(a.Paths.Root)), 0644); err != nil {
		t.Fatal(err)
	}
	check, err := a.VerifyCmdActivation(t.TempDir())
	if err != nil || !check.Passed || len(check.Tools) != 1 || check.Tools[0].Verification != "metadata" || check.Tools[0].ActualVersion != "25.1.0.12345" {
		t.Fatalf("metadata verification failed or started host: %+v %v", check, err)
	}
	if err := os.WriteFile(filepath.Join(a.Paths.Shims(), "matlab.cmd"), []byte("@echo off\r\n@echo 99.0\r\n"), 0644); err != nil {
		t.Fatal(err)
	}
	check, err = a.VerifyCmdActivation(t.TempDir())
	if err != nil || check.Passed || len(check.Tools) != 1 || check.Tools[0].Code != "shim-dispatch-failed" {
		t.Fatalf("metadata verification accepted modified shim: %+v %v", check, err)
	}
}
