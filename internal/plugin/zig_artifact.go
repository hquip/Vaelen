package plugin

import (
	"encoding/json"
	"fmt"
)

func (z *zigPlugin) Artifact(version string, target Target) (Artifact, error) {
	var index map[string]json.RawMessage
	if err := fetchJSONWithClient(httpClient, "https://ziglang.org/download/index.json", &index); err != nil {
		return Artifact{}, err
	}
	var release map[string]json.RawMessage
	if err := json.Unmarshal(index[version], &release); err != nil {
		return Artifact{}, fmt.Errorf("Zig version not available: %w", err)
	}
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[target.Arch]
	platform := map[string]string{"windows": "windows", "linux": "linux", "darwin": "macos"}[target.OS]
	var pkg struct {
		URL  string      `json:"tarball"`
		SHA  string      `json:"shasum"`
		Size json.Number `json:"size"`
	}
	if arch == "" || platform == "" {
		return Artifact{}, fmt.Errorf("unsupported Zig platform")
	}
	if err := json.Unmarshal(release[arch+"-"+platform], &pkg); err != nil {
		return Artifact{}, fmt.Errorf("Zig package missing for platform: %w", err)
	}
	size, err := pkg.Size.Int64()
	if err != nil {
		return Artifact{}, err
	}
	return Artifact{URL: pkg.URL, SHA256: pkg.SHA, Size: size, Version: version}, nil
}
