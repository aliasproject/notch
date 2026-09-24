package status

import (
	"testing"
	"time"

	"github.com/aliasproject/notch/internal/model"
)

func mkFinished(start, end time.Time) *model.Entry {
	return &model.Entry{StartTime: start, EndTime: &end}
}

func TestStartOfWeek_MondayStart(t *testing.T) {
	loc := time.FixedZone("test", -4*3600)
	cases := []struct {
		now  time.Time
		want time.Time
	}{
		{time.Date(2026, 9, 24, 15, 0, 0, 0, loc), time.Date(2026, 9, 21, 0, 0, 0, 0, loc)}, // Thursday
		{time.Date(2026, 9, 21, 0, 0, 1, 0, loc), time.Date(2026, 9, 21, 0, 0, 0, 0, loc)},  // Monday
		{time.Date(2026, 9, 27, 23, 0, 0, 0, loc), time.Date(2026, 9, 21, 0, 0, 0, 0, loc)}, // Sunday
	}
	for _, c := range cases {
		if got := StartOfWeek(c.now); !got.Equal(c.want) {
			t.Errorf("StartOfWeek(%v) = %v, want %v", c.now, got, c.want)
		}
	}
}

func TestSummarize(t *testing.T) {
	loc := time.FixedZone("test", -4*3600)
	now := time.Date(2026, 9, 24, 15, 0, 0, 0, loc) // Thursday
	entries := []*model.Entry{
		// Today, fully inside: 1h.
		mkFinished(time.Date(2026, 9, 24, 9, 0, 0, 0, loc), time.Date(2026, 9, 24, 10, 0, 0, 0, loc)),
		// Crosses midnight into today: 30m today, 1h this week.
		mkFinished(time.Date(2026, 9, 23, 23, 30, 0, 0, loc), time.Date(2026, 9, 24, 0, 30, 0, 0, loc)),
		// Earlier this week: 2h.
		mkFinished(time.Date(2026, 9, 21, 9, 0, 0, 0, loc), time.Date(2026, 9, 21, 11, 0, 0, 0, loc)),
		// Last week: none.
		mkFinished(time.Date(2026, 9, 19, 9, 0, 0, 0, loc), time.Date(2026, 9, 19, 11, 0, 0, 0, loc)),
		// Running: excluded, the consumer adds its live elapsed itself.
		{StartTime: time.Date(2026, 9, 24, 14, 0, 0, 0, loc)},
	}

	got := Summarize(entries, now)
	if want := int64(90 * 60); got.TodaySeconds != want {
		t.Errorf("TodaySeconds = %d, want %d", got.TodaySeconds, want)
	}
	if want := int64(4 * 3600); got.WeekSeconds != want {
		t.Errorf("WeekSeconds = %d, want %d", got.WeekSeconds, want)
	}
}

func TestSummarize_Empty(t *testing.T) {
	if got := Summarize(nil, time.Now()); got != (Summary{}) {
		t.Errorf("Summarize(nil) = %+v, want zero", got)
	}
}
