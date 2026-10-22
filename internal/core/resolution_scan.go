package core

import (
	"fmt"
	"os"
	"path/filepath"

	"vsm/internal/config"
	"vsm/internal/plugin"
)

// A snapshot belongs to one operation and directory; the next operation always
// reads fresh files. It does not cache across shell prompts or GUI refreshes.
type scanValue[T any] struct {
	value T
	err   error
}
type resolutionScan struct {
	start, boundary string
	projectRead     bool
	projectValue    scanValue[*Project]
	toolVersions    map[string]scanValue[*config.ToolVersions]
	workspaces      map[string]scanValue[*config.Workspace]
	installed       map[string]scanValue[[]string]
}

func newResolutionScan(start, boundary string) *resolutionScan {
	return &resolutionScan{start: start, boundary: boundary, toolVersions: map[string]scanValue[*config.ToolVersions]{}, workspaces: map[string]scanValue[*config.Workspace]{}, installed: map[string]scanValue[[]string]{}}
}

// Snapshot returns a resolver for a single synchronous request. Do not retain it
// on a long-lived App or share it between goroutines.
func (r Resolver) Snapshot(start string) Resolver {
	r.scan = newResolutionScan(start, r.boundary())
	return r
}

func (s *resolutionScan) project() (*Project, error) {
	if !s.projectRead {
		s.projectValue.value, s.projectValue.err = findProjectBounded(s.start, s.boundary)
		s.projectRead = true
	}
	return s.projectValue.value, s.projectValue.err
}
func (s *resolutionScan) versions(file string) (*config.ToolVersions, error) {
	v, ok := s.toolVersions[file]
	if !ok {
		v.value, v.err = config.Load(file)
		s.toolVersions[file] = v
	}
	return v.value, v.err
}
func (s *resolutionScan) workspace(file string) (*config.Workspace, error) {
	v, ok := s.workspaces[file]
	if !ok {
		v.value, v.err = config.LoadWorkspace(file)
		s.workspaces[file] = v
	}
	return v.value, v.err
}
func (r Resolver) loadToolVersions(file string) (*config.ToolVersions, error) {
	if r.scan != nil {
		return r.scan.versions(file)
	}
	return config.Load(file)
}
func (r Resolver) Installed(tool string) ([]string, error) {
	if r.scan == nil {
		return r.Registry.Installed(tool)
	}
	v, ok := r.scan.installed[tool]
	if !ok {
		v.value, v.err = r.Registry.Installed(tool)
		r.scan.installed[tool] = v
	}
	return append([]string(nil), v.value...), v.err
}

func (r Resolver) projectRequirement(tool, start string) (string, string, error) {
	if r.scan != nil && sameDir(start, r.scan.start) && r.boundary() == r.scan.boundary {
		return r.scan.find(tool)
	}
	return findToolVersionBounded(tool, start, r.boundary())
}

func (s *resolutionScan) findWorkspace() (*Workspace, error) {
	project, err := s.project()
	if err != nil {
		return nil, err
	}
	dir, err := filepath.Abs(s.start)
	if err != nil {
		return nil, err
	}
	for {
		file := filepath.Join(dir, config.WorkspaceName)
		c, err := s.workspace(file)
		if err == nil {
			for tool := range c.Tools {
				if _, ok := plugin.Get(tool); !ok {
					return nil, fmt.Errorf("vsm.json contains unsupported tool %s", tool)
				}
			}
			return &Workspace{Root: dir, File: file, Config: c}, nil
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
		if project != nil && sameDir(project.Root, dir) || s.boundary != "" && sameDir(s.boundary, dir) || filepath.Dir(dir) == dir {
			return nil, nil
		}
		dir = filepath.Dir(dir)
	}
}
