package core

import (
	"os"
	"path/filepath"
)

// Registry 提供对已安装版本的查询。
type Registry struct {
	Paths Paths
}

// Installed 返回某工具已安装的所有版本（目录名）。
func (r Registry) Installed(tool string) ([]string, error) {
	return dirNames(r.Paths.ToolInstalls(tool))
}

// IsInstalled 判断某版本是否已安装。
func (r Registry) IsInstalled(tool, version string) bool {
	return dirExists(r.Paths.InstallDir(tool, version))
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
