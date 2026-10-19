package plugin

import (
	"os"
	"path/filepath"
)

func mavenBinPaths(root string) []string {
	paths := []string{filepath.Join(root, "bin")}
	// Preserve versions linked to bin by previous releases.
	for _, name := range []string{"mvn", "mvn.cmd", "mvn.bat"} {
		if info, err := os.Stat(filepath.Join(root, name)); err == nil && !info.IsDir() {
			return append(paths, root)
		}
	}
	return paths
}
