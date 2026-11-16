// Package schedule decides whether a set of weekly windows is open at a given
// time, and when that will next change. It has no Kubernetes dependencies so
// it can be tested on its own.
package schedule

import (
	"fmt"
	"sort"
	"time"
)

// Window is one allowed period on the given weekdays, "HH:MM" to "HH:MM".
type Window struct {
	Days  []string
	Start string
	End   string
}

var dayNames = map[string]time.Weekday{
	"Sun": time.Sunday, "Mon": time.Monday, "Tue": time.Tuesday, "Wed": time.Wednesday,
	"Thu": time.Thursday, "Fri": time.Friday, "Sat": time.Saturday,
}

type parsed struct {
	days       map[time.Weekday]bool
	start, end time.Duration // offset from local midnight
}

// Validate checks the time zone and every window, and returns parsed windows.
func parse(tz string, windows []Window) (*time.Location, []parsed, error) {
	if tz == "" {
		tz = "UTC"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, nil, fmt.Errorf("unknown time zone %q: %w", tz, err)
	}
	if len(windows) == 0 {
		return nil, nil, fmt.Errorf("at least one window is required")
	}
	out := make([]parsed, 0, len(windows))
	for i, w := range windows {
		p := parsed{days: map[time.Weekday]bool{}}
		if len(w.Days) == 0 {
			return nil, nil, fmt.Errorf("window %d: no days", i)
		}
		for _, d := range w.Days {
			wd, ok := dayNames[d]
			if !ok {
				return nil, nil, fmt.Errorf("window %d: unknown day %q", i, d)
			}
			p.days[wd] = true
		}
		if p.start, err = clock(w.Start); err != nil {
			return nil, nil, fmt.Errorf("window %d start: %w", i, err)
		}
		if p.end, err = clock(w.End); err != nil {
			return nil, nil, fmt.Errorf("window %d end: %w", i, err)
		}
		if p.end <= p.start {
			return nil, nil, fmt.Errorf("window %d: end %s must be after start %s (split windows that cross midnight)", i, w.End, w.Start)
		}
		out = append(out, p)
	}
	return loc, out, nil
}

func clock(s string) (time.Duration, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, fmt.Errorf("%q is not HH:MM", s)
	}
	return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute, nil
}

func openAt(t time.Time, loc *time.Location, ws []parsed) bool {
	local := t.In(loc)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	for _, w := range ws {
		if !w.days[local.Weekday()] {
			continue
		}
		if !local.Before(midnight.Add(w.start)) && local.Before(midnight.Add(w.end)) {
			return true
		}
	}
	return false
}

// Evaluate reports whether rollouts are allowed at now, and the next instant
// after now when that answer changes.
func Evaluate(now time.Time, tz string, windows []Window) (open bool, next time.Time, err error) {
	loc, ws, err := parse(tz, windows)
	if err != nil {
		return false, time.Time{}, err
	}
	open = openAt(now, loc, ws)

	// Collect every window boundary in the next 8 days and return the first
	// one where the open/closed state actually flips.
	local := now.In(loc)
	var bounds []time.Time
	for d := 0; d <= 8; d++ {
		day := time.Date(local.Year(), local.Month(), local.Day()+d, 0, 0, 0, 0, loc)
		for _, w := range ws {
			if !w.days[day.Weekday()] {
				continue
			}
			bounds = append(bounds, day.Add(w.start), day.Add(w.end))
		}
	}
	sort.Slice(bounds, func(i, j int) bool { return bounds[i].Before(bounds[j]) })
	for _, b := range bounds {
		if !b.After(now) {
			continue
		}
		if openAt(b, loc, ws) != open {
			return open, b, nil
		}
	}
	// Unreachable with at least one valid window, kept as a safe fallback.
	return open, now.Add(24 * time.Hour), nil
}
