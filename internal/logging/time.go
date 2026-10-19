package logging

import "time"

// LocalTime renders diagnostics in the viewer's timezone. Stored/exported JSON
// retains its RFC3339 UTC instant so records from different machines correlate.
func LocalTime(value string) string {
	stamp, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value
	}
	return stamp.In(time.Local).Format("2006-01-02 15:04:05.000 -07:00")
}
