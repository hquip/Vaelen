package netcfg

import (
	"net/http"
	"time"

	"vsm/internal/logging"
)

type diagnosticTransport struct{ base http.RoundTripper }

func (t diagnosticTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	response, err := t.base.RoundTrip(req)
	fields := logging.Fields{"method": req.Method, "url": req.URL.String(), "duration_ms": time.Since(start).Milliseconds()}
	level := "debug"
	if response != nil {
		fields["status"] = response.StatusCode
		if response.StatusCode >= 400 {
			level = "warn"
		}
	}
	if err != nil {
		level = "warn"
		fields["error"] = err.Error()
	}
	logging.Record(level, "network", "http.request", fields)
	return response, err
}

func (t diagnosticTransport) CloseIdleConnections() {
	if closer, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}
