// Package status formats the currently-running timer for external
// consumers — status bars (waybar, polybar), shell status lines (tmux,
// i3blocks, dwm), or anything else that shells out and reads stdout. See the
// "status" subcommand in main.go for the CLI entrypoint that calls this.
package status

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/aliasproject/notch/internal/model"
)

// FormatText renders a plain-text line for shell bars (tmux status-right,
// i3blocks, dwm's xsetroot loop, etc). e is nil when no timer is running,
// which renders as an empty string so an idle bar shows nothing.
func FormatText(e *model.Entry) string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("⏱ %s · %s", e.Task, model.FormatDuration(e.Duration()))
}

// jsonOutput matches the shape waybar's "custom" module expects, plus a
// couple of additive fields (ignored by waybar, which only reads
// text/tooltip/class/percentage) that let a richer consumer -- e.g. the
// aliasOS/Omarchy bar-widget plugin -- tick a live duration client-side
// instead of re-polling this command every second.
type jsonOutput struct {
	Text        string `json:"text"`
	Tooltip     string `json:"tooltip"`
	Class       string `json:"class"`
	EntryID     int64  `json:"entryId,omitempty"`
	Task        string `json:"task,omitempty"`
	Notes       string `json:"notes,omitempty"`
	StartTime   string `json:"startTime,omitempty"` // RFC3339, empty when idle
	ProjectID   int64  `json:"projectId,omitempty"`
	ProjectName string `json:"projectName,omitempty"`
	ClientName  string `json:"clientName,omitempty"`
}

// FormatJSON renders a waybar-compatible JSON line. e is nil when no timer
// is running.
func FormatJSON(e *model.Entry) string {
	out := jsonOutput{Tooltip: "No timer running", Class: "idle"}
	if e != nil {
		out.Text = FormatText(e)
		out.Class = "running"
		out.EntryID = e.ID
		out.Task = e.Task
		out.Notes = e.Notes
		out.StartTime = e.StartTime.Format(time.RFC3339)
		out.ProjectID = e.ProjectID
		tooltip := e.Task
		if e.Project != nil && e.Project.Client != nil {
			out.ProjectName = e.Project.Name
			out.ClientName = e.Project.Client.Name
			tooltip = fmt.Sprintf("%s › %s\n%s", e.Project.Client.Name, e.Project.Name, e.Task)
		}
		out.Tooltip = tooltip
	}

	b, err := json.Marshal(out)
	if err != nil {
		// jsonOutput is a flat struct of strings — Marshal cannot fail on it.
		panic(err)
	}
	return string(b)
}
