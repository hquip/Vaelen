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
	} {
		if got := versionMatches(test.spec, test.version); got != test.want {
			t.Errorf("%s matches %s = %v, want %v", test.spec, test.version, got, test.want)
		}
	}
}
