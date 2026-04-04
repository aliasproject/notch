package model

import (
	"fmt"
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
