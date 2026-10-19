package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalTimeKeepsInstantAndShowsOffset(t *testing.T) {
	original := time.Local
	t.Cleanup(func() { time.Local = original })
	for _, test := range []struct {
		offset int
		want   string
	}{
		{8 * 3600, "2026-10-19 20:47:39.506 +08:00"},
		{-7 * 3600, "2026-10-19 05:47:39.506 -07:00"},
		{0, "2026-10-19 12:47:39.506 +00:00"},
	} {
		time.Local = time.FixedZone("test", test.offset)
		if got := LocalTime("2026-10-19T12:47:39.506233Z"); got != test.want {
			t.Fatalf("got %q, want %q", got, test.want)
		}
	}
	if got := LocalTime("invalid timestamp"); got != "invalid timestamp" {
		t.Fatal(got)
	}
}

func TestLogStorageAndExportStayUTC(t *testing.T) {
	s := testStore(t)
	Record("info", "test", "timezone", nil)
	snapshot, err := s.Read(1, "")
	if err != nil || len(snapshot.Entries) != 1 {
		t.Fatalf("%+v %v", snapshot, err)
	}
	stamp := snapshot.Entries[0].Time
	if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil || !strings.HasSuffix(stamp, "Z") {
		t.Fatalf("storage must retain UTC: %s %v", stamp, err)
	}
	dest := filepath.Join(t.TempDir(), "logs.jsonl")
	if err := s.Export(dest); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dest)
	if err != nil || !strings.Contains(string(data), stamp) {
		t.Fatalf("export changed timestamp: %s %v", data, err)
	}
}
