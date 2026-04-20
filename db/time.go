package db

import "time"

// Now returns the current time formatted for storage (UTC, ISO-8601 with ms).
func Now() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

// FormatTime formats t for storage.
func FormatTime(t time.Time) string {
	if t.IsZero() {
		return Now()
	}
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}
