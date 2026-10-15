package core

import "strings"

// splitVersion separates numeric release components, prerelease identifiers and
// ignored build metadata. It also recognizes native suffixes such as Go's rc1.
func splitVersion(version string) (release, prerelease []string) {
	version, _, _ = strings.Cut(strings.TrimPrefix(version, "v"), "+")
	base, suffix, explicit := strings.Cut(version, "-")
	release = strings.Split(base, ".")
	if explicit {
		return release, strings.Split(suffix, ".")
	}
	last := release[len(release)-1]
	i := 0
	for i < len(last) && last[i] >= '0' && last[i] <= '9' {
		i++
	}
	if i == len(last) {
		return release, nil
	}
	release[len(release)-1] = last[:i]
	suffix = last[i:]
	// Native rc/beta counters are numeric; SemVer's hyphenated identifiers above
	// keep their original lexical spelling (e.g. beta2 versus beta10).
	for _, label := range []string{"alpha", "beta", "rc", "a", "b"} {
		if counter, ok := strings.CutPrefix(suffix, label); ok && isDecimal(counter) {
			return release, []string{label, counter}
		}
	}
	return release, []string{suffix}
}

func isDecimal(value string) bool {
	if value == "" {
		return false
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// compareDecimal avoids overflow for long numeric version identifiers.
func compareDecimal(a, b string) int {
	a, b = strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return strings.Compare(a, b)
}

func compareRelease(a, b []string) int {
	for i := 0; i < max(len(a), len(b)); i++ {
		av, bv := "", ""
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		if cmp := compareDecimal(av, bv); cmp != 0 {
			return cmp
		}
	}
	return 0
}

// CompareVersions orders releases numerically and prereleases by SemVer
// precedence. A missing numeric component is zero; build metadata is ignored.
func CompareVersions(a, b string) int {
	ar, ap := splitVersion(a)
	br, bp := splitVersion(b)
	if cmp := compareRelease(ar, br); cmp != 0 {
		return cmp
	}
	if len(ap) == 0 && len(bp) == 0 {
		return 0
	}
	if len(ap) == 0 {
		return 1
	}
	if len(bp) == 0 {
		return -1
	}
	for i := 0; i < min(len(ap), len(bp)); i++ {
		an, bn := isDecimal(ap[i]), isDecimal(bp[i])
		cmp := 0
		switch {
		case an && bn:
			cmp = compareDecimal(ap[i], bp[i])
		case an:
			return -1
		case bn:
			return 1
		default:
			cmp = strings.Compare(ap[i], bp[i])
		}
		if cmp != 0 {
			return cmp
		}
	}
	if len(ap) < len(bp) {
		return -1
	}
	if len(ap) > len(bp) {
		return 1
	}
	return 0
}
