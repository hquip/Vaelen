package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"vsm/internal/config"
	"vsm/internal/netcfg"
	"vsm/internal/plugin"
)

type LockedTool struct {
	Definition string                     `json:"definition"`
	Requested  string                     `json:"requested"`
	Version    string                     `json:"version"`
	Platforms  map[string]plugin.Artifact `json:"platforms"`
}
type ToolchainLock struct {
	Schema int                   `json:"schema_version"`
	Tools  map[string]LockedTool `json:"tools"`
}

const LockName = "vsm.lock"
const artifactReceiptName = ".vsm-artifact.json"

func WorkspaceRoot(dir string) (string, error) {
	w, err := FindWorkspace(dir)
	if err != nil {
		return "", err
	}
	if w != nil {
		return w.Root, nil
	}
	p, err := FindProject(dir)
	if err != nil {
		return "", err
	}
	if p != nil {
		return p.Root, nil
	}
	return filepath.Abs(dir)
}

func ProjectRequirements(dir string) (map[string]string, error) {
	return projectRequirementsBounded(dir, os.Getenv("VSM_PROJECT_BOUNDARY"))
}

func projectRequirementsBounded(dir, boundary string) (map[string]string, error) {
	out := map[string]string{}
	for _, tool := range plugin.Names() {
		spec, src, err := findToolVersionBounded(tool, dir, boundary)
		if err != nil {
			return nil, err
		}
		if src != "" {
			out[tool] = spec
		}
	}
	return out, nil
}

func ParseTarget(value string) (plugin.Target, error) {
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return plugin.Target{}, fmt.Errorf("platform requires os/arch")
	}
	if (parts[0] != "windows" && parts[0] != "linux" && parts[0] != "darwin") || (parts[1] != "amd64" && parts[1] != "arm64") {
		return plugin.Target{}, fmt.Errorf("unsupported platform %s", value)
	}
	return plugin.Target{OS: parts[0], Arch: parts[1]}, nil
}

func validArtifact(a plugin.Artifact) error {
	switch a.Format {
	case "", "zip", "tar.gz", "tar.xz", "tgz", "binary":
	default:
		return fmt.Errorf("invalid artifact format")
	}
	if err := netcfg.ValidateURL(a.URL); err != nil {
		return err
	}
	sum, err := hex.DecodeString(a.SHA256)
	if err != nil || len(sum) != sha256.Size || a.Size < 0 || a.Size > 8<<30 {
		return fmt.Errorf("invalid locked publisher SHA256 or size")
	}
	return nil
}

func LoadLock(dir string) (*ToolchainLock, error) {
	lock, err := readLock(dir)
	if err != nil {
		return nil, err
	}
	for tool := range lock.Tools {
		if _, ok := plugin.Get(tool); !ok {
			return nil, fmt.Errorf("locked tool %s is not registered", tool)
		}
	}
	return lock, nil
}

// Parsing an old lock does not require removed tool sources to remain
// registered. Execution still uses LoadLock and validates every source.
func readLock(dir string) (*ToolchainLock, error) {
	root, err := WorkspaceRoot(dir)
	if err != nil {
		return nil, err
	}
	data, err := config.ReadBounded(filepath.Join(root, LockName), 4<<20)
	if err != nil {
		return nil, err
	}
	var lock ToolchainLock
	if err := config.DecodeStrict(data, &lock); err != nil {
		return nil, err
	}
	if lock.Schema != 1 || lock.Tools == nil || len(lock.Tools) > 256 {
		return nil, fmt.Errorf("unsupported or invalid vsm.lock")
	}
	for tool, entry := range lock.Tools {
		if err := validateInstall(tool, entry.Version); err != nil {
			return nil, err
		}
		if entry.Requested == "" || len(entry.Platforms) == 0 || len(entry.Platforms) > 6 {
			return nil, fmt.Errorf("invalid lock entry %s", tool)
		}
		for target, a := range entry.Platforms {
			if _, err := ParseTarget(target); err != nil {
				return nil, err
			}
			if err := validArtifact(a); err != nil {
				return nil, fmt.Errorf("%s/%s: %w", tool, target, err)
			}
		}
	}
	return &lock, nil
}

func (lock ToolchainLock) ValidateRequirements(dir string) error {
	return lock.validateRequirementsBounded(dir, os.Getenv("VSM_PROJECT_BOUNDARY"))
}

func (lock ToolchainLock) validateRequirementsBounded(dir, boundary string) error {
	required, err := projectRequirementsBounded(dir, boundary)
	if err != nil {
		return err
	}
	locked := map[string]string{}
	for tool, entry := range lock.Tools {
		p, ok := plugin.Get(tool)
		if !ok || entry.Definition != plugin.DefinitionID(p) {
			return fmt.Errorf("工具 %s 的安装定义发生变化；重新生成锁文件", tool)
		}
		locked[tool] = entry.Requested
	}
	if !reflect.DeepEqual(required, locked) {
		return fmt.Errorf("vsm.lock 与项目要求不一致；运行 vsm lock 更新并检查差异")
	}
	return nil
}

func ResolveToolVersion(p plugin.Plugin, spec string) (string, error) {
	if (p.Name() == "java" && regexp.MustCompile(`^(?:temurin-|zulu-)[0-9]+\.[0-9]+\.[0-9]+(?:\.[0-9]+)*(?:\+[0-9]+)?$`).MatchString(spec)) || regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:\.[0-9]+)*(?:[-+][0-9A-Za-z.-]+)?$`).MatchString(spec) {
		return spec, nil
	}
	if spec == "latest" || spec == "" {
		return p.LatestStable()
	}
	versions, err := p.ListAll()
	if err != nil {
		return "", err
	}
	version := BestMatch(spec, versions)
	if version == "" {
		return "", fmt.Errorf("%s: 没有满足 %s 的版本", p.Name(), spec)
	}
	return version, nil
}

func ResolveArtifact(p plugin.Plugin, version string, target plugin.Target) (plugin.Artifact, error) {
	if provider, ok := p.(plugin.ArtifactProvider); ok {
		a, err := provider.Artifact(version, target)
		if err != nil {
			return a, err
		}
		return a, validArtifact(a)
	}
	url, err := p.DownloadURL(version, target)
	if err != nil {
		return plugin.Artifact{}, err
	}
	sum, err := plugin.ChecksumFor(p, version, target, url)
	if err != nil {
		return plugin.Artifact{}, err
	}
	a := plugin.Artifact{URL: url, SHA256: sum}
	return a, validArtifact(a)
}

func CreateLock(dir string, targets []plugin.Target, update bool, progress ProgressFunc) (*ToolchainLock, error) {
	root, err := WorkspaceRoot(dir)
	if err != nil {
		return nil, err
	}
	required, err := ProjectRequirements(dir)
	if err != nil {
		return nil, err
	}
	if len(required) == 0 {
		return nil, fmt.Errorf("项目未声明工具版本")
	}
	if len(targets) == 0 {
		targets = []plugin.Target{plugin.CurrentTarget()}
	}
	previous, err := readLock(dir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	lock := &ToolchainLock{Schema: 1, Tools: map[string]LockedTool{}}
	names := []string{}
	for tool := range required {
		names = append(names, tool)
	}
	sort.Strings(names)
	for _, tool := range names {
		spec := required[tool]
		p, ok := plugin.Get(tool)
		if !ok {
			return nil, fmt.Errorf("工具定义 %s 已变更，请重试", tool)
		}
		entry := LockedTool{Definition: plugin.DefinitionID(p), Requested: spec, Platforms: map[string]plugin.Artifact{}}
		if !update && previous != nil {
			if old, ok := previous.Tools[tool]; ok && old.Requested == spec && old.Definition == plugin.DefinitionID(p) {
				entry = old
			}
		}
		if entry.Version == "" {
			entry.Version, err = ResolveToolVersion(p, spec)
			if err != nil {
				return nil, err
			}
		}
		for _, target := range targets {
			key := target.OS + "/" + target.Arch
			if _, err := ParseTarget(key); err != nil {
				return nil, err
			}
			if _, ok := entry.Platforms[key]; ok && !update {
				continue
			}
			if progress != nil {
				progress("锁定 " + tool + " " + entry.Version + " · " + key)
			}
			a, err := ResolveArtifact(p, entry.Version, target)
			if err != nil {
				return nil, fmt.Errorf("%s %s: %w", tool, key, err)
			}
			if a.Version != "" {
				if err := validateInstall(tool, a.Version); err != nil {
					return nil, err
				}
				if len(entry.Platforms) == 0 {
					entry.Version = a.Version
				} else if a.Version != entry.Version {
					return nil, fmt.Errorf("跨平台安装包不是同一具体构建；使用 vsm lock --update 统一更新")
				}
			}
			entry.Platforms[key] = a
		}
		lock.Tools[tool] = entry
	}
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return nil, err
	}
	guard, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer guard.Close()
	if err := config.WriteRootAtomic(guard, LockName, append(data, '\n'), 0o644); err != nil {
		return nil, err
	}
	return lock, nil
}

func CheckInstalledArtifact(dir string, want plugin.Artifact) error {
	data, err := config.ReadBounded(filepath.Join(dir, artifactReceiptName), 16<<10)
	if err != nil {
		return fmt.Errorf("无法确认已安装工具与锁文件匹配，请重新安装该版本: %w", err)
	}
	var got plugin.Artifact
	if err := config.DecodeStrict(data, &got); err != nil {
		return err
	}
	if !strings.EqualFold(got.SHA256, want.SHA256) || got.URL != want.URL {
		return fmt.Errorf("已安装工具与锁文件中的构建不一致；请先卸载后按锁文件重装")
	}
	return nil
}

func writeArtifactReceipt(dir string, artifact plugin.Artifact) error {
	if artifact.SHA256 == "" {
		return nil
	}
	data, err := json.Marshal(artifact)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	f, err := root.OpenFile(artifactReceiptName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("归档包含保留的工具来源文件: %w", err)
	}
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
