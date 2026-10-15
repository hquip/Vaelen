package core

import (
	"regexp"
	"strconv"
	"strings"
)

var constraintPart = regexp.MustCompile(`(>=|<=|>|<|=|\^|~)?\s*(v?\d+(?:\.(?:\d+|[xX*]))*(?:[-+][0-9A-Za-z.-]+)?|\*)`)

// versionMatches supports conjunctions, ||, comparisons, caret, tilde and
// dotted prefixes. Unsupported syntax never silently becomes a numeric prefix.
func versionMatches(spec, candidate string) bool {
	spec = strings.TrimSpace(spec)
	if spec == "" || spec == "latest" || spec == "*" {
		return true
	}
	if spec == candidate {
		return true
	}
	for _, alternative := range strings.Split(spec, "||") {
		rest := strings.TrimSpace(alternative)
		matched := true
		count := 0
		for rest != "" {
			rest = strings.TrimLeft(rest, " ,\t")
			if rest == "" {
				break
			}
			where := constraintPart.FindStringSubmatchIndex(rest)
			if where == nil || where[0] != 0 {
				matched = false
				break
			}
			parts := constraintPart.FindStringSubmatch(rest[:where[1]])
			op, want := parts[1], strings.TrimPrefix(parts[2], "v")
			count++
			if !matchConstraintPart(op, want, candidate) {
				matched = false
			}
			rest = rest[where[1]:]
		}
		if matched && count > 0 {
			return true
		}
	}
	return false
}

func matchConstraintPart(op, want, have string) bool {
	if want == "*" {
		return op == "" || op == "="
	}
	if strings.ContainsAny(want, "xX*") {
		if op != "" && op != "=" {
			return false
		}
		parts := strings.Split(want, ".")
		var prefix []string
		for _, part := range parts {
			if part == "x" || part == "X" || part == "*" {
				break
			}
			prefix = append(prefix, part)
		}
		return versionHasPrefix(have, strings.Join(prefix, "."))
	}
	comparison := CompareVersions(have, want)
	switch op {
	case "":
		return versionHasPrefix(have, want)
	case "=":
		return comparison == 0
	case ">=":
		return comparison >= 0
	case "<=":
		return comparison <= 0
	case ">":
		return comparison > 0
	case "<":
		return comparison < 0
	case "^", "~":
		if comparison < 0 {
			return false
		}
		parts := strings.Split(want, ".")
		numbers := make([]int, 3)
		for i := 0; i < len(parts) && i < 3; i++ {
			numbers[i] = atoiSafe(parts[i])
		}
		index := 0
		if op == "~" && len(parts) > 1 {
			index = 1
		}
		if op == "^" {
			for index < 2 && index < len(parts)-1 && numbers[index] == 0 {
				index++
			}
		}
		numbers[index]++
		for i := index + 1; i < 3; i++ {
			numbers[i] = 0
		}
		upper := strconv.Itoa(numbers[0]) + "." + strconv.Itoa(numbers[1]) + "." + strconv.Itoa(numbers[2])
		return CompareVersions(have, upper) < 0
	}
	return false
}
