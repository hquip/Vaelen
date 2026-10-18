package netcfg

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"vsm/internal/logging"
)

type fakeLogTransport func(*http.Request) (*http.Response, error)

func (f fakeLogTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNetworkDiagnosticsExcludeCredentialsHeadersAndBody(t *testing.T) {
	t.Setenv("VSM_HOME", t.TempDir())
	t.Setenv("VSM_LOG_LEVEL", "debug")
	request, err := http.NewRequest("POST", "https://private-user:private-password@example.org/api?token=private-query", strings.NewReader("private-body"))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer private-header")
	transport := diagnosticTransport{fakeLogTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer private-header" {
			t.Fatal("logging changed the request")
		}
		return &http.Response{StatusCode: 403, Body: http.NoBody}, nil
	})}
	response, err := transport.RoundTrip(request)
	if err != nil || response.StatusCode != 403 {
		t.Fatalf("logging changed response: %v", err)
	}
	store, _ := logging.Default()
	snapshot, err := store.Read(100, "")
	if err != nil || len(snapshot.Entries) != 1 || snapshot.Entries[0].Level != "warn" || snapshot.Entries[0].Fields["status"] != float64(403) {
		t.Fatalf("missing HTTP diagnostic: %+v %v", snapshot, err)
	}
	raw, _ := json.Marshal(snapshot)
	if strings.Contains(string(raw), "private-") {
		t.Fatal("HTTP diagnostics leaked secrets")
	}
}
