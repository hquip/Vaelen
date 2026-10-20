package updater

import (
	"context"
	"strings"

	"vsm/internal/netcfg"
	"vsm/internal/plugin"
)

type downloadNetwork struct {
	proxy   string
	mirrors []string
}

func automaticDownloadNetwork(ctx context.Context, asset Asset) downloadNetwork {
	route, err := netcfg.RouteForURL(ctx, asset.URL)
	if err == nil && route.Proxy != "" {
		return downloadNetwork{proxy: route.Source}
	}
	mode := plugin.MirrorMode()
	if mode == "off" {
		return downloadNetwork{proxy: "direct"}
	}
	var mirrors []string
	if values, err := plugin.CustomMirrors(); err == nil {
		if base := values["github"]; base != "" && plugin.ValidateMirror("github", base) == nil {
			mirrors = append(mirrors, strings.TrimRight(base, "/")+"/"+asset.URL)
		}
	}
	if mode != "custom" {
		mirrors = append(mirrors, "https://ghfast.top/"+asset.URL, "https://gh-proxy.com/"+asset.URL)
	}
	if len(mirrors) > 2 {
		mirrors = mirrors[:2]
	}
	return downloadNetwork{proxy: "direct", mirrors: mirrors}
}
