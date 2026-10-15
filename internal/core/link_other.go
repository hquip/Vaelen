//go:build !windows

package core

import "os"

func platformLinkDir(target, link string) error { return os.Symlink(target, link) }
