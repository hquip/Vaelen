package core

import "testing"

func TestVersionConstraintBoundaries(t *testing.T) {
	for _, test := range []struct {
		spec, version string
		want          bool
	}{
		{"<20", "20.11.1", false}, {"<20", "19.9.0", true}, {">=3.10,<3.12", "3.11.9", true}, {">=3.10,<3.12", "3.12.0", false},
		{"^20.1.0", "20.9.0", true}, {"^20.1.0", "21.0.0", false}, {"~1.2.3", "1.3.0", false}, {"20.x", "20.11.0", true},
		{"18 || >=20 <21", "20.3.0", true}, {"18 || >=20 <21", "19.0.0", false}, {"evil20", "20.0.0", false},
		{"^1.2.3", "1.2.3-rc.1", false}, {"latest", "1.3.0-beta.1", false}, {"1.2", "1.2.4-rc.1", false},
		{"*", "1.2.3-rc.1", false}, {"1.x", "1.0.0-rc.1", false}, {">=0 *", "0.0.0-alpha", false},
		{">=1.2.3-rc.1 <2", "1.2.3-rc.2", true}, {">=1.2.3-rc.1 <2", "1.3.0-rc.1", false},
		{"^1.2.3-rc.1", "1.2.3", true}, {"1.2.3-rc.1", "1.2.3-rc.1", true}, {"1.2.3", "1.2.3+build.4", true},
	} {
		if got := versionMatches(test.spec, test.version); got != test.want {
			t.Errorf("%s matches %s = %v, want %v", test.spec, test.version, got, test.want)
		}
	}
}

func TestBestMatchRequiresExplicitPrereleases(t *testing.T) {
	for _, spec := range []string{"latest", "", "*", "1", "^1.2.3"} {
		if got := BestMatch(spec, []string{"1.3.0-rc.1", "1.2.4", "1.2.3"}); got != "1.2.4" {
			t.Fatalf("%q selected prerelease or older version: %q", spec, got)
		}
		if got := BestMatch(spec, []string{"1.3.0-rc.1"}); got != "" {
			t.Fatalf("%q silently adopted an unrequested prerelease: %q", spec, got)
		}
	}
	if got := BestMatch("1.3.0-rc.1", []string{"1.3.0-rc.1", "1.2.4"}); got != "1.3.0-rc.1" {
		t.Fatal("explicit prerelease is unavailable")
	}
}
