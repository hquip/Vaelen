package logging

import (
	"net/url"
	"os"
	"regexp"
	"strings"
)

var (
	urlPattern      = regexp.MustCompile(`(?i)(?:https?|socks5h?)://[^\s"'<>]+`)
	secretPattern   = regexp.MustCompile(`(?i)((?:[\w-]*(?:password|passwd|token|secret|api[_-]?key)|authorization)\s*[=:]\s*)(?:"[^"]*"|'[^']*'|[^\s,;]+)`)
	bearerPattern   = regexp.MustCompile(`(?i)\b(Bearer|Basic)\s+[A-Za-z0-9._~+/=-]+`)
	userinfoPattern = regexp.MustCompile(`[^\s:/@"'<>]+:[^\s@"'<>]+@`)
)

func sensitiveKey(key string) bool {
	key = strings.ToLower(key)
	for _, word := range []string{"password", "passwd", "token", "secret", "authorization", "api_key", "apikey", "api-key", "cookie"} {
		if strings.Contains(key, word) {
			return true
		}
	}
	return false
}

// Redact removes URL credentials, complete queries/fragments and common secret
// assignments. Known secret environment values are also removed from errors.
func Redact(value string) string {
	for _, pair := range os.Environ() {
		key, secret, _ := strings.Cut(pair, "=")
		if sensitiveKey(key) && len(secret) >= 4 {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	value = urlPattern.ReplaceAllStringFunc(value, func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil {
			return "[REDACTED_URL]"
		}
		if u.User != nil {
			u.User = url.User("REDACTED")
		}
		if u.RawQuery != "" || u.ForceQuery {
			u.RawQuery = "REDACTED"
			u.ForceQuery = false
		}
		if u.Fragment != "" {
			u.Fragment = "REDACTED"
		}
		return u.String()
	})
	// Invalid scheme-less proxy inputs may still be echoed by URL validation.
	value = userinfoPattern.ReplaceAllString(value, "REDACTED@")
	value = bearerPattern.ReplaceAllString(value, "$1 [REDACTED]")
	value = secretPattern.ReplaceAllString(value, "$1[REDACTED]")
	return value
}
