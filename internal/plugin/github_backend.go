package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

func DefinitionID(p Plugin) string {
	data := []byte("native-v1:" + p.Name())
	if m, ok := p.(manifestPlugin); ok {
		data, _ = json.Marshal(m.m)
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

type CatalogTool struct {
	Name        string      `json:"name"`
	Installable bool        `json:"installable"`
	Source      string      `json:"source"`
	Commands    []string    `json:"commands"`
	Support     ToolSupport `json:"support"`
}

func Catalog(query string) []CatalogTool {
	out := []CatalogTool{}
	query = strings.ToLower(query)
	for _, name := range Names() {
		p, ok := Get(name)
		if !ok {
			continue
		}
		source, installable := "native", true
		if m, ok := p.(manifestPlugin); ok {
			source = m.m.Source.Type + ":" + m.m.Source.Repo
			installable = m.m.Source.Type == "github-auto" || len(m.m.Assets) > 0
		}
		if query != "" && !MatchesTool(name, query) && !strings.Contains(strings.ToLower(source), query) {
			continue
		}
		support := Support(name, CurrentTarget())
		installable = installable && (support.Mode == "install" || support.Mode == "release")
		out = append(out, CatalogTool{Name: name, Installable: installable, Source: source, Commands: DetectCommands()[name], Support: support})
	}
	return out
}

func GitHubManifest(name, repo, command, bin, match string) (Manifest, error) {
	if !regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`).MatchString(repo) {
		return Manifest{}, fmt.Errorf("GitHub source requires owner/repo")
	}
	if command == "" {
		command = name
	}
	m := Manifest{Name: name, Source: ManifestSource{Type: "github-auto", Repo: repo}, Bin: bin, Commands: []string{command}, Match: match}
	if err := validateManifest(m); err != nil {
		return m, err
	}
	return m, nil
}

func (p manifestPlugin) Artifact(version string, target Target) (Artifact, error) {
	if p.m.Source.Type == "github-auto" {
		return githubArtifact(p.m, version, target)
	}
	raw, err := p.DownloadURL(version, target)
	if err != nil {
		return Artifact{}, err
	}
	sum, err := ChecksumFor(p, version, target, raw)
	return Artifact{URL: raw, SHA256: sum}, err
}

func githubArtifact(m Manifest, version string, target Target) (Artifact, error) {
	var release struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name   string `json:"name"`
			URL    string `json:"browser_download_url"`
			Digest string `json:"digest"`
			Size   int64  `json:"size"`
		} `json:"assets"`
	}
	endpoint := "https://api.github.com/repos/" + m.Source.Repo + "/releases/tags/" + url.PathEscape(version)
	if err := fetchJSONOfficial(endpoint, &release); err != nil {
		if !strings.HasPrefix(version, "v") && strings.Contains(err.Error(), "HTTP 404") {
			if err := fetchJSONOfficial("https://api.github.com/repos/"+m.Source.Repo+"/releases/tags/"+url.PathEscape("v"+version), &release); err != nil {
				return Artifact{}, err
			}
		} else {
			return Artifact{}, err
		}
	}
	if release.Draft || strings.TrimPrefix(release.Tag, "v") != strings.TrimPrefix(version, "v") {
		return Artifact{}, fmt.Errorf("GitHub returned a different or draft release")
	}
	var selected []Artifact
	for _, a := range release.Assets {
		if !matchesReleasePlatform(a.Name, target) || m.Match != "" && !strings.Contains(a.Name, m.Match) {
			continue
		}
		format := assetFormat(a.Name)
		if format == "" {
			continue
		}
		digest := strings.TrimPrefix(a.Digest, "sha256:")
		decoded, err := hex.DecodeString(digest)
		if err != nil || len(decoded) != sha256.Size || !strings.HasPrefix(a.Digest, "sha256:") {
			continue
		}
		u, err := url.Parse(a.URL)
		if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			continue
		}
		prefix := "/" + m.Source.Repo + "/releases/download/" + release.Tag + "/"
		if !strings.HasPrefix(u.Path, prefix) || strings.TrimPrefix(u.Path, prefix) != a.Name || a.Size <= 0 || a.Size > 8<<30 {
			continue
		}
		selected = append(selected, Artifact{URL: a.URL, SHA256: strings.ToLower(digest), Size: a.Size, Format: format, Version: release.Tag})
	}
	if len(selected) != 1 {
		return Artifact{}, fmt.Errorf("%s %s/%s 匹配到 %d 个具有官方摘要的包；请用 --match 明确筛选，或配置 assets", m.Name, target.OS, target.Arch, len(selected))
	}
	return selected[0], nil
}

func assetFormat(name string) string {
	name = strings.ToLower(name)
	for _, ext := range []string{".tar.gz", ".tar.xz", ".zip", ".tgz"} {
		if strings.HasSuffix(name, ext) {
			return strings.TrimPrefix(ext, ".")
		}
	}
	for _, unsupported := range []string{".tar.bz2", ".bz2", ".7z", ".rar", ".zst"} {
		if strings.HasSuffix(name, unsupported) {
			return ""
		}
	}
	return "binary"
}

func matchesReleasePlatform(name string, target Target) bool {
	lower := strings.ToLower(name)
	for _, bad := range []string{"checksum", "sha256", "sha512", ".asc", ".sig", ".json", ".txt", ".deb", ".rpm", ".msi", ".pkg", ".dmg", "android", "freebsd", "musl", "debug", "symbols"} {
		if strings.Contains(lower, bad) {
			return false
		}
	}
	tokens := regexp.MustCompile(`[^a-z0-9]+`).Split(lower, -1)
	set := map[string]bool{}
	for _, t := range tokens {
		set[t] = true
	}
	osMatch := false
	switch target.OS {
	case "windows":
		osMatch = set["windows"] || set["win"] || set["win64"] || set["mingw"]
	case "darwin":
		osMatch = set["darwin"] || set["macos"] || set["mac"] || set["osx"] || set["apple"]
	case "linux":
		osMatch = set["linux"]
	}
	archMatch := false
	switch target.Arch {
	case "amd64":
		archMatch = set["amd64"] || set["x64"] || strings.Contains(lower, "x86_64") || strings.Contains(lower, "x86-64")
	case "arm64":
		archMatch = set["arm64"] || set["aarch64"]
	}
	return osMatch && archMatch
}

func (p manifestPlugin) autoVersions() ([]string, error) {
	owner, repo := splitRepo(p.m.Source.Repo)
	releases, err := githubReleases(owner, repo)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, r := range releases {
		if !r.Prerelease {
			out = append(out, r.TagName)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return compareVer(strings.TrimPrefix(out[i], "v"), strings.TrimPrefix(out[j], "v")) > 0
	})
	return out, nil
}
