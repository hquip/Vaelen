package core

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"
)

// validateComponent deliberately uses portable rules on every platform.
func validateComponent(s string) error {
	if s == "" || s == "." || s == ".." || strings.TrimRight(s, ". ") != s || strings.ContainsAny(s, "/\\:<>\"|?*") {
		return fmt.Errorf("unsafe path component: %q", s)
	}
	for _, c := range s {
		if unicode.IsControl(c) {
			return fmt.Errorf("unsafe control character in %q", s)
		}
	}
	base := strings.ToUpper(strings.SplitN(s, ".", 2)[0])
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || base == "CONIN$" || base == "CONOUT$" || (len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9') {
		return fmt.Errorf("reserved device name: %q", s)
	}
	return nil
}

func validateInstall(tool, version string) error {
	if strings.HasPrefix(tool, ".") || strings.HasPrefix(version, ".") {
		return fmt.Errorf("tool and version cannot start with a dot")
	}
	for _, s := range []string{tool, version} {
		for _, c := range s {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._+-", c)) {
				return fmt.Errorf("invalid tool/version identifier: %q", s)
			}
		}
	}
	if err := validateComponent(tool); err != nil {
		return err
	}
	return validateComponent(version)
}

// Reject linked tool directories. Only individual version entries may link out.
func (p Paths) openInstalls(tool string, create bool) (*os.Root, error) {
	if err := validateComponent(tool); err != nil {
		return nil, err
	}
	if create {
		if err := os.MkdirAll(p.Installs(), 0o755); err != nil {
			return nil, err
		}
	}
	r, err := os.OpenRoot(p.Installs())
	if err != nil {
		return nil, err
	}
	if create {
		if err := r.Mkdir(tool, 0o755); err != nil && !os.IsExist(err) {
			r.Close()
			return nil, err
		}
	}
	fi, err := r.Lstat(tool)
	if err != nil {
		r.Close()
		return nil, err
	}
	if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		r.Close()
		return nil, fmt.Errorf("tool directory must be a real directory: %s", tool)
	}
	if _, err := r.Readlink(tool); err == nil {
		r.Close()
		return nil, fmt.Errorf("tool directory cannot be a junction")
	}
	child, err := r.OpenRoot(tool)
	r.Close()
	return child, err
}

func pathWithin(root, target string) bool {
	a, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	b, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		a = strings.ToLower(a)
		b = strings.ToLower(b)
	}
	rel, err := filepath.Rel(a, b)
	return err == nil && (rel == "." || filepath.IsLocal(rel))
}

// Resolve existing ancestors too, so an alias cannot bypass layout isolation.
func canonicalPath(name string) (string, error) {
	abs, err := filepath.Abs(name)
	if err != nil {
		return "", err
	}
	var suffix []string
	for cur := abs; ; cur = filepath.Dir(cur) {
		real, err := platformCanonicalExistingPath(cur)
		if err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				real = filepath.Join(real, suffix[i])
			}
			return real, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		if filepath.Dir(cur) == cur {
			return "", err
		}
		suffix = append(suffix, filepath.Base(cur))
	}
}
