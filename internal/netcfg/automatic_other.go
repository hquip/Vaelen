//go:build !windows && !darwin

package netcfg

func systemProxyCandidates() []proxyCandidate { return nil }
