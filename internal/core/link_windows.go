package core

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var finalPathByHandle = syscall.NewLazyDLL("kernel32.dll").NewProc("GetFinalPathNameByHandleW")

// EvalSymlinks may keep junction paths unchanged. Resolve the opened object's
// physical path, including mount points, before comparing directory boundaries.
func platformCanonicalExistingPath(name string) (string, error) {
	f, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]uint16, 32768)
	n, _, callErr := finalPathByHandle.Call(f.Fd(), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0)
	if n == 0 {
		return "", fmt.Errorf("resolve directory handle: %w", callErr)
	}
	if n >= uintptr(len(buf)) {
		return "", fmt.Errorf("resolved Windows path exceeds limit")
	}
	path := syscall.UTF16ToString(buf[:n])
	if strings.HasPrefix(path, `\\?\UNC\`) {
		path = `\\` + strings.TrimPrefix(path, `\\?\UNC\`)
	} else {
		path = strings.TrimPrefix(path, `\\?\`)
	}
	return filepath.Clean(path), nil
}

// Create the junction using the Windows API, never cmd.exe. Shell metacharacters
// and %VAR% sequences in legal paths must remain literal directory names.
func platformLinkDir(target, link string) error {
	if err := os.Mkdir(link, 0o755); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			os.Remove(link)
		}
	}()
	name, err := syscall.UTF16PtrFromString(link)
	if err != nil {
		return err
	}
	h, err := syscall.CreateFile(name, syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_OPEN_REPARSE_POINT|syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(h)
	substitute := `\??\` + target
	if strings.HasPrefix(target, `\\`) {
		substitute = `\??\UNC\` + strings.TrimPrefix(target, `\\`)
	}
	sub, err := syscall.UTF16FromString(substitute)
	if err != nil {
		return err
	}
	printName, err := syscall.UTF16FromString(target)
	if err != nil {
		return err
	}
	data := make([]byte, 16+2*(len(sub)+len(printName)))
	if len(data) > 16*1024 {
		return fmt.Errorf("junction target exceeds Windows reparse buffer limit")
	}
	binary.LittleEndian.PutUint32(data[0:4], 0xa0000003) // IO_REPARSE_TAG_MOUNT_POINT
	binary.LittleEndian.PutUint16(data[4:6], uint16(len(data)-8))
	binary.LittleEndian.PutUint16(data[10:12], uint16((len(sub)-1)*2))
	binary.LittleEndian.PutUint16(data[12:14], uint16(len(sub)*2))
	binary.LittleEndian.PutUint16(data[14:16], uint16((len(printName)-1)*2))
	for i, c := range append(sub, printName...) {
		binary.LittleEndian.PutUint16(data[16+i*2:], c)
	}
	var returned uint32
	if err := syscall.DeviceIoControl(h, 0x000900a4, &data[0], uint32(len(data)), nil, 0, &returned, nil); err != nil {
		return err
	}
	committed = true
	return nil
}
