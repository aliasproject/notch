package status

import (
	"time"

	"github.com/aliasproject/notch/internal/model"
)

// Summary is the tracked time for today and for this week (Monday start,
// matching the TUI's own "this week" preset), in the local timezone. Only
// finished entries count: like "projects"' totalSeconds, a consumer showing
// a live total adds the running entry's own elapsed time on top, so it can
// tick locally instead of re-polling.
type Summary struct {
	TodaySeconds int64 `json:"todaySeconds"`
	WeekSeconds  int64 `json:"weekSeconds"`
}

// StartOfDay is local midnight on now's date.
func StartOfDay(now time.Time) time.Time {
	y, m, d := now.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, now.Location())
}

// StartOfWeek is local midnight on the Monday of now's week.
func StartOfWeek(now time.Time) time.Time {
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	return StartOfDay(now).AddDate(0, 0, -(weekday - 1))
}

// Summarize totals entries into today/this-week buckets as of now. An
// entry that crosses a boundary (say, one left running past midnight)
// counts only the part inside each period, not its whole duration.
func Summarize(entries []*model.Entry, now time.Time) Summary {
	day, week := StartOfDay(now), StartOfWeek(now)
	var s Summary
	for _, e := range entries {
		if e.EndTime == nil {
			continue
		}
		s.TodaySeconds += overlapSeconds(e.StartTime, *e.EndTime, day, now)
		s.WeekSeconds += overlapSeconds(e.StartTime, *e.EndTime, week, now)
	}
	return s
}

func overlapSeconds(start, end, from, to time.Time) int64 {
	if start.Before(from) {
		start = from
	}
	if end.After(to) {
		end = to
	}
	if !end.After(start) {
		return 0
	}
	return int64(end.Sub(start) / time.Second)
}
