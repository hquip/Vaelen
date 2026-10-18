package config

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
)

// WriteRootAtomic keeps temporary creation and final replacement under an open
// filesystem root, including when an intermediate directory is a symlink.
func WriteRootAtomic(root *os.Root, name string, data []byte, mode os.FileMode) error {
	if !filepath.IsLocal(name) {
		return fmt.Errorf("file escapes root")
	}
	parent := filepath.Dir(name)
	if err := root.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	if fi, err := root.Lstat(name); err == nil {
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("refusing to replace linked or special configuration")
		}
		mode = fi.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return err
	}
	tmp := filepath.Join(parent, ".vsm-write-"+rand.Text())
	f, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer root.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return root.Rename(tmp, name)
}
