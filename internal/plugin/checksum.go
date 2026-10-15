package plugin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"vsm/internal/netcfg"
)

// ChecksumFor obtains integrity metadata from the original publisher, never a mirror.
func ChecksumFor(p Plugin, version string, target Target, downloadURL string) (string, error) {
	if m, ok := p.(manifestPlugin); ok {
		if sum := m.m.Checksums[version+"/"+target.OS+"/"+target.Arch]; sum != "" {
			return sum, nil
		}
	}
	if cs, ok := p.(Checksummer); ok {
		return cs.Checksum(version, target)
	}
	u, err := url.Parse(downloadURL)
	if err != nil {
		return "", err
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if u.Hostname() != "github.com" || len(parts) < 6 || parts[2] != "releases" || parts[3] != "download" {
		return "", nil
	}
	api := "https://api.github.com/repos/" + parts[0] + "/" + parts[1] + "/releases/tags/" + url.PathEscape(parts[4])
	var rel ghRelease
	if err := fetchJSONOfficial(api, &rel); err != nil {
		return "", err
	}
	for _, asset := range rel.Assets {
		if asset.Name == path.Base(u.Path) {
			return strings.TrimPrefix(asset.Digest, "sha256:"), nil
		}
	}
	return "", fmt.Errorf("checksum metadata does not contain the requested release asset")
}

func fetchOfficial(raw string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "vsm")
	resp, err := netcfg.Client(2 * time.Minute).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("official checksum metadata: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (16<<20)+1))
	if len(data) > 16<<20 {
		return nil, fmt.Errorf("checksum metadata exceeds limit")
	}
	return data, err
}

func fetchJSONOfficial(raw string, result any) error {
	data, err := fetchOfficial(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, result)
}

func (n *nodePlugin) Checksum(version string, target Target) (string, error) {
	raw, err := n.DownloadURL(version, target)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	data, err := fetchOfficial("https://nodejs.org/dist/v" + version + "/SHASUMS256.txt")
	if err != nil {
		return "", err
	}
	return checksumLine(string(data), path.Base(u.Path))
}

func checksumLine(text, filename string) (string, error) {
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == filename {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("official checksum metadata has no entry for %s", filename)
}
