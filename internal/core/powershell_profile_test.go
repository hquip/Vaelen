package core

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func profileBytes(text, encoding string) []byte {
	if encoding == "utf8" {
		return []byte(text)
	}
	if encoding == "utf8bom" {
		return append([]byte{0xef, 0xbb, 0xbf}, []byte(text)...)
	}
	var order binary.ByteOrder = binary.LittleEndian
	bom := []byte{0xff, 0xfe}
	if encoding == "utf16be" {
		order, bom = binary.BigEndian, []byte{0xfe, 0xff}
	}
	units := utf16.Encode([]rune(text))
	data := make([]byte, 2+len(units)*2)
	copy(data, bom)
	for i, u := range units {
		order.PutUint16(data[2+i*2:], u)
	}
	return data
}

func TestPowerShellProfilePreservesEncodingAndCustomizations(t *testing.T) {
	for _, encoding := range []string{"utf8", "utf8bom", "utf16le", "utf16be"} {
		t.Run(encoding, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "profile.ps1")
			prefix, suffix := "# 我的终端 😀\r\nfunction prompt { 'dev> ' }\r\n", "\r\nSet-Alias ll Get-ChildItem\r\n"
			old := prefix + hookBlockBegin + "\r\n& 'old-vsm' activate powershell\r\n" + hookBlockEnd + suffix
			if err := os.WriteFile(path, profileBytes(old, encoding), 0o644); err != nil {
				t.Fatal(err)
			}
			line := PowerShellActivationCommand(filepath.Join(t.TempDir(), "it's $work", "vsm.exe"))
			if status, err := persistPowerShellBlock(path, line); err != nil || status != "updated" {
				t.Fatalf("%s %v", status, err)
			}
			got, err := os.ReadFile(path)
			want := profileBytes(prefix+hookBlockBegin+"\r\n"+line+"\r\n"+hookBlockEnd+suffix, encoding)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("encoding/custom commands changed: %q %v", got, err)
			}
			if status, err := persistPowerShellBlock(path, line); err != nil || status != "exists" {
				t.Fatalf("not idempotent: %s %v", status, err)
			}
		})
	}
}

func TestPowerShellProfileNewFileAndAppend(t *testing.T) {
	for _, original := range []string{"", "# custom\nWrite-Output 'hello'"} {
		path := filepath.Join(t.TempDir(), "redirected", "profile.ps1")
		if original != "" {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if status, err := persistPowerShellBlock(path, "activate"); err != nil || status != "added" {
			t.Fatalf("%s %v", status, err)
		}
		got, _ := os.ReadFile(path)
		if original == "" && !bytes.HasPrefix(got, []byte{0xef, 0xbb, 0xbf}) {
			t.Fatal("new PS5 profile needs a UTF-8 BOM")
		}
		if !strings.HasPrefix(string(bytes.TrimPrefix(got, []byte{0xef, 0xbb, 0xbf})), original) {
			t.Fatal("custom content lost")
		}
	}
}

func TestPowerShellProfileRejectsUnsafeEdits(t *testing.T) {
	fixtures := [][]byte{
		[]byte(hookBlockBegin), []byte(hookBlockEnd + "\n" + hookBlockBegin),
		[]byte(hookBlockBegin + hookBlockBegin + hookBlockEnd),
		[]byte("# SIG # Begin signature block\n# signed content"),
		{0xff, 0xfe, 1}, {0xff, 0xfe, 0, 0xd8}, {0xff, 0xfe, 0, 0xdc}, {0xff, 0x80},
	}
	for _, fixture := range fixtures {
		path := filepath.Join(t.TempDir(), "profile.ps1")
		if err := os.WriteFile(path, fixture, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := persistPowerShellBlock(path, "activate"); err == nil {
			t.Fatalf("unsafe edit accepted: %q", fixture)
		}
		got, _ := os.ReadFile(path)
		if !bytes.Equal(got, fixture) {
			t.Fatal("failed edit changed original profile")
		}
	}
}
