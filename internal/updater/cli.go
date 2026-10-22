package updater

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// InstallCLI repairs a missing GUI companion from the exact official release.
// It never updates an unrelated vsm.exe found on PATH or selects a newer tag.
func (c Client) InstallCLI(ctx context.Context, version, directory string) (string, error) {
	info, err := c.Check(ctx, version, version)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	unlock, err := acquireUpdateLock(directory)
	if err != nil {
		return "", fmt.Errorf("配套 CLI 正在准备或目录不可写: %w", err)
	}
	defer unlock()
	stage, err := os.MkdirTemp(directory, ".cli-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	// Use the official asset name beside the GUI. A stale canonical vsm.exe is
	// preserved, and later `vsm update --gui` still updates the adjacent pair.
	name := cliAsset()
	file := filepath.Join(stage, name)
	asset, err := info.asset(cliAsset())
	if err != nil {
		return "", err
	}
	c.fileIndex, c.fileCount, c.currentAsset, c.currentSize, c.totalBytes = 1, 1, asset.Name, asset.Size, asset.Size
	if err := c.download(ctx, info, asset.Name, file); err != nil {
		return "", err
	}
	if err := validateBinary(ctx, file, "vsm", info.Latest); err != nil {
		return "", err
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	target := filepath.Join(directory, name)
	if _, err := replaceFiles(info.Latest, stage, []replacement{{Target: target, Staged: file}}); err != nil {
		return "", err
	}
	return target, nil
}
