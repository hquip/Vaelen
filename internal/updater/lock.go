package updater

import (
	"fmt"
	"os"
	"path/filepath"
)

// Keep the lock inode in place: unlinking it after unlock can let two processes
// lock different inodes at the same path. OS locks release on process death.
// Existing empty lock files from older releases are safe to reuse.
func acquireUpdateLock(directory string) (func(), error) {
	file, err := os.OpenFile(filepath.Join(directory, ".vsm-update.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("更新目录不可写: %w", err)
	}
	unlock, err := lockUpdateFile(file)
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("已有更新或配套 CLI 准备任务: %w", err)
	}
	return func() { unlock(); file.Close() }, nil
}
