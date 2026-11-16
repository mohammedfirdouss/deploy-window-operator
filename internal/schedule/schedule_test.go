package schedule

import (
	"testing"
	"time"
)

const (
	morningStart  = "09:00"
	morningTime   = "10:00"
	noon          = "12:00"
	mondayClosing = "2026-11-16T17:00:00Z"
	monday        = "Mon"
	utc           = "UTC"
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
	weekdays := []Window{{Days: []string{monday, "Tue", "Wed", "Thu", "Fri"}, Start: morningStart, End: "17:00"}}

	tests := []struct {
		name     string
		now      string
		tz       string
		windows  []Window
		wantOpen bool
		wantNext string
	}{
		{"inside window", "2026-11-16T10:00:00Z", utc, weekdays, true, mondayClosing},
		{"before window", "2026-11-16T08:00:00Z", utc, weekdays, false, "2026-11-16T09:00:00Z"},
		{"at start is open", "2026-11-16T09:00:00Z", utc, weekdays, true, mondayClosing},
		{"at end is closed", mondayClosing, utc, weekdays, false, "2026-11-17T09:00:00Z"},
		{"friday evening waits for monday", "2026-11-20T18:00:00Z", utc, weekdays, false, "2026-11-23T09:00:00Z"},
		{"weekend closed", "2026-11-21T12:00:00Z", utc, weekdays, false, "2026-11-23T09:00:00Z"},
		// 09:30 UTC is 10:30 in Lagos (UTC+1): open, closes at 17:00 Lagos = 16:00 UTC.
		{"time zone applied", "2026-11-16T09:30:00Z", "Africa/Lagos", weekdays, true, "2026-11-16T16:00:00Z"},
		{
			"back-to-back windows merge", "2026-11-16T11:30:00Z", utc,
			[]Window{
				{Days: []string{monday}, Start: morningStart, End: noon},
				{Days: []string{monday}, Start: noon, End: "15:00"},
			},
			true, "2026-11-16T15:00:00Z",
		},
		{
			"two windows with a gap", "2026-11-16T12:30:00Z", utc,
			[]Window{
				{Days: []string{monday}, Start: morningStart, End: noon},
				{Days: []string{monday}, Start: "14:00", End: "16:00"},
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
		{"unknown zone", "Mars/Olympus", []Window{{Days: []string{monday}, Start: morningStart, End: morningTime}}},
		{"no windows", utc, nil},
		{"bad day", utc, []Window{{Days: []string{"Funday"}, Start: morningStart, End: morningTime}}},
		{"bad time", utc, []Window{{Days: []string{monday}, Start: "9am", End: morningTime}}},
		{"end before start", utc, []Window{{Days: []string{monday}, Start: "22:00", End: "02:00"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := Evaluate(now, tc.tz, tc.windows); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
