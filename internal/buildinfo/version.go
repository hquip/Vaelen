// Package buildinfo shares the release version between the CLI and desktop app.
package buildinfo

import "strings"

// Version is overridden by release builds using -ldflags -X.
var Version = "1.0.1"

func DisplayVersion() string { return "v" + strings.TrimPrefix(Version, "v") }
