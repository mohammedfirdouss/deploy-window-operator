package schedule

import (
	"testing"
	"time"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestEvaluate(t *testing.T) {
	weekdays := []Window{{Days: []string{"Mon", "Tue", "Wed", "Thu", "Fri"}, Start: "09:00", End: "17:00"}}

	tests := []struct {
		name     string
		now      string
		tz       string
		windows  []Window
		wantOpen bool
		wantNext string
	}{
		{"inside window", "2026-11-16T10:00:00Z", "UTC", weekdays, true, "2026-11-16T17:00:00Z"},
		{"before window", "2026-11-16T08:00:00Z", "UTC", weekdays, false, "2026-11-16T09:00:00Z"},
		{"at start is open", "2026-11-16T09:00:00Z", "UTC", weekdays, true, "2026-11-16T17:00:00Z"},
		{"at end is closed", "2026-11-16T17:00:00Z", "UTC", weekdays, false, "2026-11-17T09:00:00Z"},
		{"friday evening waits for monday", "2026-11-20T18:00:00Z", "UTC", weekdays, false, "2026-11-23T09:00:00Z"},
		{"weekend closed", "2026-11-21T12:00:00Z", "UTC", weekdays, false, "2026-11-23T09:00:00Z"},
		// 09:30 UTC is 10:30 in Lagos (UTC+1): open, closes at 17:00 Lagos = 16:00 UTC.
		{"time zone applied", "2026-11-16T09:30:00Z", "Africa/Lagos", weekdays, true, "2026-11-16T16:00:00Z"},
		{
			"back-to-back windows merge", "2026-11-16T11:30:00Z", "UTC",
			[]Window{
				{Days: []string{"Mon"}, Start: "09:00", End: "12:00"},
				{Days: []string{"Mon"}, Start: "12:00", End: "15:00"},
			},
			true, "2026-11-16T15:00:00Z",
		},
		{
			"two windows with a gap", "2026-11-16T12:30:00Z", "UTC",
			[]Window{
				{Days: []string{"Mon"}, Start: "09:00", End: "12:00"},
				{Days: []string{"Mon"}, Start: "14:00", End: "16:00"},
			},
			false, "2026-11-16T14:00:00Z",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			open, next, err := Evaluate(mustTime(t, tc.now), tc.tz, tc.windows)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if open != tc.wantOpen {
				t.Errorf("open = %v, want %v", open, tc.wantOpen)
			}
			if want := mustTime(t, tc.wantNext); !next.Equal(want) {
				t.Errorf("next = %s, want %s", next.UTC().Format(time.RFC3339), tc.wantNext)
			}
		})
	}
}

func TestEvaluateErrors(t *testing.T) {
	now := mustTime(t, "2026-11-16T10:00:00Z")
	tests := []struct {
		name    string
		tz      string
		windows []Window
	}{
		{"unknown zone", "Mars/Olympus", []Window{{Days: []string{"Mon"}, Start: "09:00", End: "10:00"}}},
		{"no windows", "UTC", nil},
		{"bad day", "UTC", []Window{{Days: []string{"Funday"}, Start: "09:00", End: "10:00"}}},
		{"bad time", "UTC", []Window{{Days: []string{"Mon"}, Start: "9am", End: "10:00"}}},
		{"end before start", "UTC", []Window{{Days: []string{"Mon"}, Start: "22:00", End: "02:00"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := Evaluate(now, tc.tz, tc.windows); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
