package core

import (
	"os"
	"path/filepath"
	"strings"
)

// Registry 提供对已安装版本的查询。
type Registry struct {
	Paths Paths
}

// Installed 返回某工具已安装的所有版本（目录名）。
func (r Registry) Installed(tool string) ([]string, error) {
	if err := validateComponent(tool); err != nil {
		return nil, err
	}
	guard, err := r.Paths.openInstalls(tool, false)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	guard.Close()
	names, err := dirNames(r.Paths.ToolInstalls(tool))
	if err != nil {
		return nil, err
	}
	containers := r.installStorageContainers(tool, names)
	versions := names[:0]
	for _, name := range names {
		if !containers[name] {
			versions = append(versions, name)
		}
	}
	return versions, nil
}

// Older custom installations could place their targets below the tool registry
// itself (go/go/1.27.3). The first directory is storage, not a second version.
// Identify it from verified ownership receipts rather than guessing by name.
func (r Registry) installStorageContainers(tool string, names []string) map[string]bool {
	containers := map[string]bool{}
	root, err := canonicalPath(r.Paths.ToolInstalls(tool))
	if err != nil {
		return containers
	}
	for _, version := range names {
		dir := r.Paths.InstallDir(tool, version)
		if !isLink(dir) {
			continue
		}
		linkedTarget, err := os.Readlink(dir)
		if err != nil {
			continue
		}
		if !filepath.IsAbs(linkedTarget) {
			linkedTarget = filepath.Join(filepath.Dir(dir), linkedTarget)
		}
		if _, owned, err := ownedReceipt(dir, linkedTarget, tool, version); err != nil || !owned {
			continue
		}
		target, err := canonicalPath(linkedTarget)
		if err != nil || !pathWithin(root, target) {
			continue
		}
		rel, err := filepath.Rel(root, target)
		if err != nil {
			continue
		}
		if container, _, nested := strings.Cut(rel, string(filepath.Separator)); nested {
			containers[container] = true
		}
	}
	return containers
}

// IsInstalled 判断某版本是否已安装。
func (r Registry) IsInstalled(tool, version string) bool {
	names, err := r.Installed(tool)
	if err != nil {
		return false
	}
	for _, name := range names {
		if name == version {
			return true
		}
	}
	return false
}

// Tools 返回有安装记录的工具列表。
func (r Registry) Tools() ([]string, error) {
	return dirNames(r.Paths.Installs())
}

// dirNames 返回 dir 下所有“目录型”条目名。会跟随符号链接/junction，
// 因为版本目录可能是指向真实安装位置的 junction（见激活设计）。
func dirNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		// 点开头的条目是 vsm 的内部暂存（如安装中的 .1.22.0.partial），不是工具或版本
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if isDirFollow(filepath.Join(dir, e.Name()), e) {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// isDirFollow 判断条目是否为目录；若条目本身不是普通目录（可能是 junction/
// 符号链接），则跟随解析其目标再判断。
func isDirFollow(path string, e os.DirEntry) bool {
	if e.IsDir() {
		return true
	}
	info, err := os.Stat(path) // os.Stat 会跟随链接
	return err == nil && info.IsDir()
}
