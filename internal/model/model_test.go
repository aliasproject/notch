package model

import (
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"zero", 0, "0:00:00"},
		{"seconds", 45 * time.Second, "0:00:45"},
		{"minutes", 5*time.Minute + 30*time.Second, "0:05:30"},
		{"exact hour", time.Hour, "1:00:00"},
		{"hour boundary minus a second", time.Hour - time.Second, "0:59:59"},
		{"many hours", 123*time.Hour + 4*time.Minute + 5*time.Second, "123:04:05"},
		{"rounds sub-second up", 59*time.Second + 600*time.Millisecond, "0:01:00"},
		{"rounds sub-second down", 59*time.Second + 400*time.Millisecond, "0:00:59"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := FormatDuration(c.d); got != c.want {
				t.Errorf("FormatDuration(%v) = %q, want %q", c.d, got, c.want)
			}
		})
	}
}

func TestEntryDuration_Finished(t *testing.T) {
	start := time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)
	end := start.Add(90 * time.Minute)
	e := &Entry{StartTime: start, EndTime: &end}

	if got, want := e.Duration(), 90*time.Minute; got != want {
		t.Errorf("Duration() = %v, want %v", got, want)
	}
}

func TestEntryDuration_Running(t *testing.T) {
	e := &Entry{StartTime: time.Now().Add(-2 * time.Second)}

	got := e.Duration()
	if got < 2*time.Second || got > 5*time.Second {
		t.Errorf("Duration() = %v, want roughly 2s (running entry measured against time.Now)", got)
	}
}

func TestEntryIsRunning(t *testing.T) {
	start := time.Now()
	running := &Entry{StartTime: start}
	if !running.IsRunning() {
		t.Error("IsRunning() = false for entry with nil EndTime, want true")
	}

	end := start.Add(time.Hour)
	finished := &Entry{StartTime: start, EndTime: &end}
	if finished.IsRunning() {
		t.Error("IsRunning() = true for entry with set EndTime, want false")
	}
}

func TestEntryEarnings(t *testing.T) {
	start := time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	e := &Entry{StartTime: start, EndTime: &end}

	if got, want := e.Earnings(50), 100.0; got != want {
		t.Errorf("Earnings(50) = %v, want %v", got, want)
	}
	if got, want := e.Earnings(0), 0.0; got != want {
		t.Errorf("Earnings(0) = %v, want %v", got, want)
	}
}
