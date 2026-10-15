package core

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type installReceipt struct{ Tool, Version, Target, Token string }

func receiptName(version string) string { return "." + version + ".vsm.json" }

// The registry-side receipt is independent of the downloaded archive and target.
func createReceipt(root, dest, target, tool, version string) (installReceipt, error) {
	abs, err := filepath.Abs(target)
	if err != nil {
		return installReceipt{}, err
	}
	r := installReceipt{tool, version, abs, rand.Text()}
	data, err := json.Marshal(r)
	if err != nil {
		return r, err
	}
	f, err := os.OpenFile(filepath.Join(root, installMarker), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return r, fmt.Errorf("写入安装标记失败: %w", err)
	}
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil {
		return r, err
	}
	if closeErr != nil {
		return r, closeErr
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return r, err
	}
	parent, err := os.OpenRoot(filepath.Dir(dest))
	if err != nil {
		return r, err
	}
	defer parent.Close()
	f, err = parent.OpenFile(receiptName(version), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return r, err
	}
	_, err = f.Write(data)
	closeErr = f.Close()
	if err != nil {
		parent.Remove(receiptName(version))
		return r, err
	}
	if closeErr != nil {
		parent.Remove(receiptName(version))
		return r, closeErr
	}
	return r, nil
}

func ownedReceipt(dir, target, tool, version string) (installReceipt, bool, error) {
	parent, err := os.OpenRoot(filepath.Dir(dir))
	if err != nil {
		return installReceipt{}, false, err
	}
	defer parent.Close()
	data, err := parent.ReadFile(receiptName(version))
	if os.IsNotExist(err) {
		return installReceipt{}, false, nil
	}
	if err != nil {
		return installReceipt{}, false, err
	}
	var r installReceipt
	if err := json.Unmarshal(data, &r); err != nil {
		return r, false, err
	}
	if r.Tool != tool || r.Version != version || r.Token == "" || !sameDir(r.Target, target) || filepath.Base(r.Target) != version || filepath.Base(filepath.Dir(r.Target)) != tool {
		return r, false, fmt.Errorf("installation ownership receipt does not match target")
	}
	root, err := os.OpenRoot(target)
	if os.IsNotExist(err) {
		return r, true, nil
	}
	if err != nil {
		return r, false, err
	}
	defer root.Close()
	marker, err := root.ReadFile(installMarker)
	if err != nil {
		return r, false, err
	}
	var m installReceipt
	if err := json.Unmarshal(marker, &m); err != nil {
		return r, false, err
	}
	if m != r {
		return r, false, fmt.Errorf("installation marker does not match registry receipt")
	}
	return r, true, nil
}

func removeOwnedTarget(r installReceipt) error {
	parent, err := os.OpenRoot(filepath.Dir(r.Target))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer parent.Close()
	name := filepath.Base(r.Target)
	fi, err := parent.Lstat(name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("owned target was replaced by a link or non-directory")
	}
	if _, err := parent.Readlink(name); err == nil {
		return fmt.Errorf("owned target was replaced by a junction")
	}
	return parent.RemoveAll(name)
}
