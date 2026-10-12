package model

import (
	"fmt"
	"strings"
	"time"
)

type Client struct {
	ID         int64
	Name       string
	HourlyRate float64
	CreatedAt  time.Time
}

type Project struct {
	ID        int64
	ClientID  int64
	Client    *Client
	Name      string
	CreatedAt time.Time
}

type Entry struct {
	ID        int64
	ProjectID int64
	Project   *Project
	Task      string
	StartTime time.Time
	EndTime   *time.Time
	Invoiced  bool
	Paid      bool
	Notes     string
	CreatedAt time.Time
}

func (e *Entry) Duration() time.Duration {
	if e.EndTime != nil {
		return e.EndTime.Sub(e.StartTime)
	}
	return time.Since(e.StartTime)
}

func (e *Entry) IsRunning() bool {
	return e.EndTime == nil
}

func (e *Entry) Earnings(hourlyRate float64) float64 {
	return e.Duration().Hours() * hourlyRate
}

func FormatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%d:%02d:%02d", h, m, s)
}

type ReportRow struct {
	ClientName  string
	ProjectName string
	ClientID    int64
	ProjectID   int64
	HourlyRate  float64
	TotalHours  float64
	Earnings    float64
	EntryCount  int
	Invoiced    int
	Paid        int
	Entries     []*Entry // populated after load
}

// RecentTask is one distinct (project, task) pairing that's been tracked
// before, with its accumulated time across every finished entry -- for
// pickers that want to resume a specific named task rather than just start
// a bare project (see db.ListRecentTasks).
type RecentTask struct {
	ProjectID    int64
	ProjectName  string
	ClientName   string
	Task         string
	TotalSeconds int64
	LastUsed     time.Time
	// Billing status across the task's finished entries, so a picker can
	// show "2/3 invoiced" without listing every entry. Paid implies
	// invoiced, so PaidCount <= InvoicedCount <= EntryCount.
	EntryCount    int
	InvoicedCount int
	PaidCount     int
}

// ParseEditTime parses a time given on the command line: RFC3339 (what
// scripts and the bar-widget plugin send) or the TUI's own local
// "YYYY-MM-DD HH:MM" form.
func ParseEditTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04", s, time.Local); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("%q is not RFC3339 or YYYY-MM-DD HH:MM", s)
}
