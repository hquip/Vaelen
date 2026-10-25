package plugin

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

func init() {
	register(releaseLanguage{"godot", "godotengine/godot"})
	register(releaseLanguage{"vyper", "vyperlang/vyper"})
	register(nimSDK{})
}

type releaseLanguage struct{ name, repo string }

func (p releaseLanguage) Name() string { return p.name }
func (p releaseLanguage) BinPaths(root string) []string {
	if p.name == "godot" {
		return []string{root, filepath.Join(root, "bin"), filepath.Join(root, "Godot.app", "Contents", "MacOS"), filepath.Join(root, "Contents", "MacOS")}
	}
	return []string{root, filepath.Join(root, "bin")}
}
func (p releaseLanguage) ExecEnv(string) map[string]string { return nil }
func (p releaseLanguage) ListAll() ([]string, error) {
	owner, repo := splitRepo(p.repo)
	releases, err := githubReleases(owner, repo)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, r := range releases {
		if r.Prerelease {
			continue
		}
		v := strings.TrimPrefix(r.TagName, "v")
		if p.name == "godot" {
			if !strings.HasSuffix(v, "-stable") {
				continue
			}
			v = strings.TrimSuffix(v, "-stable")
		}
		if releaseLanguageVersion.MatchString(v) {
			seen[v] = true
		}
	}
	return sortedLanguageVersions(seen), nil
}
func (p releaseLanguage) LatestStable() (string, error) {
	versions, err := p.ListAll()
	if err != nil {
		return "", err
	}
	if len(versions) == 0 {
		return "", fmt.Errorf("%s 没有稳定发行版", p.name)
	}
	return versions[0], nil
}
func (p releaseLanguage) DownloadURL(v string, target Target) (string, error) {
	a, err := p.Artifact(v, target)
	return a.URL, err
}

var releaseLanguageVersion = regexp.MustCompile(`^\d+\.\d+(?:\.\d+)?$`)

func (p releaseLanguage) Artifact(v string, target Target) (Artifact, error) {
	if !releaseLanguageVersion.MatchString(v) {
		return Artifact{}, fmt.Errorf("%s 需要完整稳定版本号", p.name)
	}
	if target.Arch != "amd64" && target.Arch != "arm64" {
		return Artifact{}, fmt.Errorf("%s 不支持架构 %s", p.name, target.Arch)
	}
	tag, format := "v"+v, "binary"
	var selectName func(string) bool
	if p.name == "godot" {
		tag, format = v+"-stable", "zip"
		platform := map[string]string{"windows/amd64": "win64", "windows/arm64": "windows_arm64", "linux/amd64": "linux.x86_64", "linux/arm64": "linux.arm64", "darwin/amd64": "macos.universal", "darwin/arm64": "macos.universal"}[target.OS+"/"+target.Arch]
		if platform == "" {
			return Artifact{}, fmt.Errorf("Godot 不支持 %s/%s", target.OS, target.Arch)
		}
		want := "Godot_v" + tag + "_" + platform + ".zip"
		if target.OS == "windows" {
			want = "Godot_v" + tag + "_" + platform + ".exe.zip"
		}
		selectName = func(name string) bool { return name == want }
	} else {
		platform := map[string]string{"windows": "windows.exe", "linux": "linux", "darwin": "darwin"}[target.OS]
		if platform == "" || target.Arch != "amd64" {
			return Artifact{}, fmt.Errorf("Vyper 独立发行包仅适配 amd64；其它架构请纳管已有安装")
		}
		pattern := regexp.MustCompile(`^vyper\.` + regexp.QuoteMeta(v) + `\+commit\.[0-9a-f]+\.` + regexp.QuoteMeta(platform) + `$`)
		selectName = pattern.MatchString
	}
	return publisherReleaseArtifact(p.repo, tag, format, selectName)
}

// Pin the exact asset and its publisher digest. Similar names, source archives,
// draft releases and redirects to unrelated repositories are never accepted.
func publisherReleaseArtifact(repo, tag, format string, selectName func(string) bool) (Artifact, error) {
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
	if err := fetchJSONOfficial("https://api.github.com/repos/"+repo+"/releases/tags/"+url.PathEscape(tag), &release); err != nil {
		return Artifact{}, err
	}
	if release.Tag != tag || release.Draft || release.Prerelease {
		return Artifact{}, fmt.Errorf("发行方返回了不同版本或非稳定发行版")
	}
	var selected []Artifact
	for _, a := range release.Assets {
		if !selectName(a.Name) {
			continue
		}
		digest := strings.TrimPrefix(a.Digest, "sha256:")
		bytes, err := hex.DecodeString(digest)
		if err != nil || len(bytes) != 32 || !strings.HasPrefix(a.Digest, "sha256:") {
			return Artifact{}, fmt.Errorf("%s 缺少有效的发行方 SHA256；请纳管已有安装", a.Name)
		}
		u, err := url.Parse(a.URL)
		if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/"+repo+"/releases/download/"+tag+"/"+a.Name || a.Size <= 0 || a.Size > 8<<30 {
			return Artifact{}, fmt.Errorf("发行方归档地址或大小无效")
		}
		selected = append(selected, Artifact{URL: a.URL, Version: tag, Format: format, Size: a.Size, SHA256: strings.ToLower(digest)})
	}
	if len(selected) != 1 {
		return Artifact{}, fmt.Errorf("%s %s 匹配到 %d 个目标包；不支持的平台或历史版本可纳管已有安装", repo, tag, len(selected))
	}
	return selected[0], nil
}

// Godot's public command is stable even though its archive executable contains
// a version suffix. Keep the signed macOS .app unchanged and add an outer link.
func (p releaseLanguage) PrepareInstall(root, v string, target Target) (string, error) {
	if p.name != "godot" {
		name := "vyper"
		if target.OS == "windows" {
			name += ".exe"
		}
		if err := validateVyperExecutable(filepath.Join(root, name), target); err != nil {
			return "", err
		}
		return root, nil
	}
	if !releaseLanguageVersion.MatchString(v) {
		return "", fmt.Errorf("invalid Godot version")
	}
	prefix := "Godot_v" + v + "-stable_"
	regular := func(name string) error {
		info, err := os.Lstat(filepath.Join(root, name))
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("Godot executable is not a regular file: %s", name)
		}
		return nil
	}
	rename := func(old, name string) error {
		if err := regular(old); err != nil {
			return err
		}
		if _, err := os.Lstat(filepath.Join(root, name)); !os.IsNotExist(err) {
			return fmt.Errorf("Godot command destination already exists")
		}
		return os.Rename(filepath.Join(root, old), filepath.Join(root, name))
	}
	switch target.OS {
	case "windows":
		platform := map[string]string{"amd64": "win64", "arm64": "windows_arm64"}[target.Arch]
		if platform == "" {
			return "", fmt.Errorf("unsupported Godot architecture")
		}
		if err := rename(prefix+platform+".exe", "godot.exe"); err != nil {
			return "", err
		}
		if err := rename(prefix+platform+"_console.exe", "godot_console.exe"); err != nil {
			return "", err
		}
	case "linux":
		platform := map[string]string{"amd64": "linux.x86_64", "arm64": "linux.arm64"}[target.Arch]
		if platform == "" {
			return "", fmt.Errorf("unsupported Godot architecture")
		}
		if err := rename(prefix+platform, "godot"); err != nil {
			return "", err
		}
		if err := os.Chmod(filepath.Join(root, "godot"), 0755); err != nil {
			return "", err
		}
	case "darwin":
		bundle, err := os.Lstat(filepath.Join(root, "Godot.app"))
		if err != nil || !bundle.IsDir() || bundle.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("missing real Godot.app bundle")
		}
		if err := regular("Godot.app/Contents/MacOS/Godot"); err != nil {
			return "", err
		}
		if err := os.Symlink("Godot.app/Contents/MacOS/Godot", filepath.Join(root, "godot")); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("unsupported Godot platform")
	}
	return root, nil
}

// These release names omit the CPU architecture. Check the actual bounded
// executable header after verifying the complete publisher hash, before exposing
// a command or executing it. Future publisher build-runner changes fail clearly.
func validateVyperExecutable(file string, target Target) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	buffer := make([]byte, 4096)
	n, _ := f.Read(buffer)
	data := buffer[:n]
	match := false
	switch target.OS {
	case "windows":
		if len(data) >= 64 && string(data[:2]) == "MZ" {
			offset := uint64(binary.LittleEndian.Uint32(data[60:64]))
			if offset+6 <= uint64(len(data)) && string(data[offset:offset+4]) == "PE\x00\x00" {
				machine := binary.LittleEndian.Uint16(data[offset+4 : offset+6])
				match = target.Arch == "amd64" && machine == 0x8664 || target.Arch == "arm64" && machine == 0xaa64
			}
		}
	case "linux":
		if len(data) >= 20 && string(data[:4]) == "\x7fELF" && data[4] == 2 && data[5] == 1 {
			machine := binary.LittleEndian.Uint16(data[18:20])
			match = target.Arch == "amd64" && machine == 62 || target.Arch == "arm64" && machine == 183
		}
	case "darwin":
		if len(data) >= 8 && binary.LittleEndian.Uint32(data[:4]) == 0xfeedfacf {
			machine := binary.LittleEndian.Uint32(data[4:8])
			match = target.Arch == "amd64" && machine == 0x1000007 || target.Arch == "arm64" && machine == 0x100000c
		}
	}
	if !match {
		return fmt.Errorf("Vyper 官方独立包与 %s/%s 的可执行格式不匹配；请纳管已有本机安装", target.OS, target.Arch)
	}
	return nil
}

type nimSDK struct{}

func (nimSDK) Name() string                     { return "nim" }
func (nimSDK) BinPaths(root string) []string    { return []string{filepath.Join(root, "bin"), root} }
func (nimSDK) ExecEnv(string) map[string]string { return nil }

// Follow the publisher's stable download table, not the nightly repository's
// newest tag. That repository hosts both stable binaries and development builds.
func nimArchives() (map[string][]string, error) {
	data, err := fetchOfficial("https://nim-lang.org/install.html")
	if err != nil {
		return nil, err
	}
	result := map[string][]string{}
	for _, match := range regexp.MustCompile(`href="(https://[^"<>\s]+)"`).FindAllStringSubmatch(string(data), -1) {
		raw := match[1]
		u, err := url.Parse(raw)
		if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			continue
		}
		if !(u.Host == "nim-lang.org" && strings.HasPrefix(u.Path, "/download/") || u.Host == "github.com" && strings.HasPrefix(u.Path, "/nim-lang/nightlies/releases/download/")) {
			continue
		}
		m := regexp.MustCompile(`^nim-(\d+\.\d+\.\d+)(?:-(?:windows|linux|macosx))?_(?:x64|arm64)\.(?:zip|tar\.xz)$`).FindStringSubmatch(path.Base(u.Path))
		if m == nil {
			continue
		}
		// Odd minor channels are development snapshots, even if linked by a page.
		minor := strings.Split(m[1], ".")[1]
		if (minor[len(minor)-1]-'0')%2 != 0 {
			continue
		}
		result[m[1]] = append(result[m[1]], raw)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("Nim 官网未返回可识别的稳定版归档；请检查网络或纳管已有安装")
	}
	return result, nil
}
func sortedLanguageVersions(seen map[string]bool) []string {
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return compareVer(out[i], out[j]) > 0 })
	return out
}
func (nimSDK) ListAll() ([]string, error) {
	archives, err := nimArchives()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for v := range archives {
		seen[v] = true
	}
	return sortedLanguageVersions(seen), nil
}
func (n nimSDK) LatestStable() (string, error) {
	versions, err := n.ListAll()
	if err != nil {
		return "", err
	}
	if len(versions) == 0 {
		return "", fmt.Errorf("Nim 官网未返回稳定二进制包")
	}
	return versions[0], nil
}
func (n nimSDK) DownloadURL(v string, target Target) (string, error) {
	a, err := n.Artifact(v, target)
	return a.URL, err
}
func (nimSDK) Artifact(v string, target Target) (Artifact, error) {
	if !sdkVersion.MatchString(v) {
		return Artifact{}, fmt.Errorf("Nim 需要完整稳定版本号")
	}
	platform := map[string]string{"windows/amd64": "windows_x64.zip", "linux/amd64": "linux_x64.tar.xz", "linux/arm64": "linux_arm64.tar.xz", "darwin/amd64": "macosx_x64.tar.xz", "darwin/arm64": "macosx_arm64.tar.xz"}[target.OS+"/"+target.Arch]
	if platform == "" {
		return Artifact{}, fmt.Errorf("Nim 不支持 %s/%s 自动安装", target.OS, target.Arch)
	}
	archives, err := nimArchives()
	if err != nil {
		return Artifact{}, err
	}
	for _, raw := range archives[v] {
		name := path.Base(raw)
		if name != "nim-"+v+"-"+platform && !(target.OS == "windows" && name == "nim-"+v+"_x64.zip") {
			continue
		}
		data, err := fetchOfficial(raw + ".sha256")
		var sum string
		if err != nil {
			// The stable table can retain links to removed checksum sidecars.
			// Use the same asset's publisher API digest, never a computed hash.
			if strings.HasPrefix(raw, "https://github.com/") && strings.Contains(err.Error(), "HTTP 404") {
				sum, err = ChecksumFor(nimSDK{}, v, target, raw)
			}
		} else {
			sum, err = checksumLine(string(data), name)
		}
		if err != nil {
			return Artifact{}, err
		}
		decoded, err := hex.DecodeString(sum)
		if err != nil || len(decoded) != 32 {
			return Artifact{}, fmt.Errorf("Nim 官方 SHA256 无效")
		}
		return Artifact{URL: raw, SHA256: strings.ToLower(sum), Format: assetFormat(name), Version: v}, nil
	}
	return Artifact{}, fmt.Errorf("Nim %s 没有 %s/%s 的官方稳定归档", v, target.OS, target.Arch)
}
