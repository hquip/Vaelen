package plugin

import (
	"strings"
	"testing"
)

func TestChecksumSelectsExactArtifact(t *testing.T) {
	sum := strings.Repeat("a", 64)
	text := strings.Repeat("b", 64) + "  node-v1-linux-arm64.tar.gz\n" + sum + " *node-v1-linux-x64.tar.gz\n"
	got, err := checksumLine(text, "node-v1-linux-x64.tar.gz")
	if err != nil || got != sum {
		t.Fatalf("%s %v", got, err)
	}
	if _, err := checksumLine(text, "node-v1-linux-x64.tar.gz.evil"); err == nil {
		t.Fatal("accepted non-exact asset")
	}
}
