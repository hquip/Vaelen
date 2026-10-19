package database

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"vsm/internal/config"
)

func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// Open every managed directory without accepting symlink/junction redirection.
func (m *Manager) ensure(parts ...string) error {
	if !filepath.IsAbs(m.Root) {
		return fmt.Errorf("数据库存储路径必须为绝对路径")
	}
	if err := os.MkdirAll(m.Root, 0700); err != nil {
		return err
	}
	paths := []string{m.Root}
	p := m.Root
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, "/\\\x00") {
			return fmt.Errorf("无效数据库目录")
		}
		p = filepath.Join(p, part)
		paths = append(paths, p)
	}
	for _, p := range paths {
		if err := os.Mkdir(p, 0700); err != nil && !os.IsExist(err) {
			return err
		}
		fi, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("数据库目录不能是链接: %s", p)
		}
		if _, err := os.Readlink(p); err == nil {
			return fmt.Errorf("数据库目录不能是 junction: %s", p)
		}
	}
	return nil
}
func (m *Manager) path(name string, parts ...string) string {
	return filepath.Join(append([]string{m.Root, "instances", name}, parts...)...)
}
func (m *Manager) versionPath(i *Instance, parts ...string) string {
	return m.path(i.Name, append([]string{"versions", i.Backend + "-" + i.Version}, parts...)...)
}
func (m *Manager) prepareVersion(i *Instance) error {
	return m.ensure("instances", i.Name, "versions", i.Backend+"-"+i.Version)
}

func (m *Manager) load(name string) (*Instance, error) {
	if !namePattern.MatchString(name) {
		return nil, fmt.Errorf("实例名称使用小写字母开头，后接字母、数字、- 或 _")
	}
	if err := m.checkDirectories(name); err != nil {
		return nil, err
	}
	data, err := config.ReadBounded(m.path(name, "instance.json"), 1<<20)
	if err != nil {
		return nil, err
	}
	var i Instance
	if err := config.DecodeStrict(data, &i); err != nil {
		return nil, err
	}
	if i.Name != name {
		return nil, fmt.Errorf("数据库实例名称与存储位置不符")
	}
	if err := validate(i); err != nil {
		return nil, err
	}
	return &i, nil
}
func (m *Manager) checkDirectories(name string) error {
	paths := []string{m.Root, filepath.Join(m.Root, "instances")}
	if name != "" {
		paths = append(paths, m.path(name))
	}
	for _, path := range paths {
		fi, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("数据库目录不能是链接: %s", path)
		}
		if _, err := os.Readlink(path); err == nil {
			return fmt.Errorf("数据库目录不能是 junction: %s", path)
		}
	}
	return nil
}
func (m *Manager) save(i *Instance) error {
	if err := validate(*i); err != nil {
		return err
	}
	if err := m.ensure("instances", i.Name); err != nil {
		return err
	}
	data, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(m.path(i.Name, "instance.json"), append(data, '\n'), 0600)
}
func (m *Manager) credentials(i *Instance) (credential, error) {
	path := m.path(i.Name, "credentials.json")
	data, err := config.ReadBounded(path, 4096)
	if err == nil {
		var c credential
		if err := config.DecodeStrict(data, &c); err != nil {
			return c, err
		}
		if !ownerPattern.MatchString(c.Password) {
			return c, fmt.Errorf("数据库凭据损坏")
		}
		m.remember(c.Password)
		return c, nil
	}
	if !os.IsNotExist(err) {
		return credential{}, err
	}
	// Missing credentials after initialization must never silently reset a password.
	for _, v := range i.Versions {
		if v.Initialized || v.Allocated {
			return credential{}, fmt.Errorf("数据库凭据丢失，请恢复 %s", path)
		}
	}
	password, err := randomID()
	if err != nil {
		return credential{}, err
	}
	c := credential{Password: password}
	m.remember(c.Password)
	data, _ = json.Marshal(c)
	return c, config.WriteFileAtomic(path, data, 0600)
}

func (m *Manager) saveVersion(i *Instance) error {
	stored, err := m.load(i.Name)
	if err != nil {
		return err
	}
	if stored.Owner != i.Owner {
		return fmt.Errorf("数据库实例所有权已变化")
	}
	stored.Versions[i.Version] = i.Versions[i.Version]
	return m.save(stored)
}
func (m *Manager) lock(name string) (func(), error) {
	if !namePattern.MatchString(name) {
		return nil, fmt.Errorf("无效数据库实例名称")
	}
	if err := m.ensure("instances", name); err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(m.path(name))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	if _, err := r.Readlink(".lock"); err == nil {
		return nil, fmt.Errorf("数据库锁文件不能是链接")
	}
	f, err := r.OpenFile(".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	unlock, err := lockDatabase(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	return func() { unlock(); f.Close() }, nil
}
