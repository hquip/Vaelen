package core

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.22.0", "1.21.5", 1},
		{"1.21.5", "1.22.0", -1},
		{"1.22.0", "1.22.0", 0},
		{"1.22", "1.22.0", 0},
		{"20.11.1", "20.9.0", 1},
		{"1.8.0", "11.0.2", -1},
		{"0-rc1", "0", -1},
		{"1.2.3-rc.1", "1.2.3", -1},
		{"1.2.3", "1.2.3-rc.1", 1},
		{"1.2.3-rc.2", "1.2.3-rc.10", -1},
		{"1.2.3-1", "1.2.3-alpha", -1},
		{"1.2.3-alpha", "1.2.3-alpha.1", -1},
		{"1.2.3+build.1", "1.2.3+build.99", 0},
		{"1.26rc2", "1.26rc10", -1},
		{"1.26beta2", "1.26rc1", -1},
		{"1.26rc10", "1.26.0", -1},
		{"v1.2.3", "1.2.3", 0},
		{"1.2.99999999999999999999999", "1.2.100000000000000000000000", -1},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q,%q)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestBestMatch(t *testing.T) {
	inst := []string{"1.22.0", "1.21.5", "1.20.10", "20.11.1"}
	cases := []struct{ spec, want string }{
		{"1.22.0", "1.22.0"},
		{"1.21", "1.21.5"},
		{"1", "1.22.0"},
		{"latest", "20.11.1"},
		{"", "20.11.1"},
		{"9", ""},
	}
	for _, c := range cases {
		if got := BestMatch(c.spec, inst); got != c.want {
			t.Errorf("BestMatch(%q)=%q want %q", c.spec, got, c.want)
		}
	}
	if got := BestMatch("1", nil); got != "" {
		t.Errorf("BestMatch on empty should be empty, got %q", got)
	}
}

func TestVersionHasPrefix(t *testing.T) {
	if !versionHasPrefix("20.11.1", "20") {
		t.Error("20 应是 20.11.1 的前缀")
	}
	if versionHasPrefix("2.0.1", "20") {
		t.Error("20 不应是 2.0.1 的前缀")
	}
	if !versionHasPrefix("1.22.0", "1.22") {
		t.Error("1.22 应是 1.22.0 的前缀")
	}
}
