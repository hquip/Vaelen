//go:build !windows

package core

import (
	"os"
	"path/filepath"
)

func platformLinkDir(target, link string) error { return os.Symlink(target, link) }

func platformCanonicalExistingPath(name string) (string, error) { return filepath.EvalSymlinks(name) }
