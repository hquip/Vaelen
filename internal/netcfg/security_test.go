package netcfg

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestResourceURLPolicy(t *testing.T) {
	t.Setenv("VSM_ALLOW_INSECURE_HTTP", "")
	for _, raw := range []string{"file:///etc/passwd", "http://example.com/tool.zip", "https://user:secret@example.com/file"} {
		if ValidateURL(raw) == nil {
			t.Fatalf("accepted unsafe resource URL %s", raw)
		}
	}
	for _, raw := range []string{"https://example.com/tool.zip", "http://127.0.0.1/test", "http://[::1]/test"} {
		if err := ValidateURL(raw); err != nil {
			t.Fatal(err)
		}
	}
	from, _ := url.Parse("https://example.com/file")
	to, _ := url.Parse("http://127.0.0.1/file")
	if err := Client(time.Second).CheckRedirect(&http.Request{URL: to}, []*http.Request{{URL: from}}); err == nil {
		t.Fatal("allowed HTTPS downgrade")
	}
}
