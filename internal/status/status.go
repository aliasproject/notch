// Package status formats the currently-running timer for external
// consumers — status bars (waybar, polybar), shell status lines (tmux,
// i3blocks, dwm), or anything else that shells out and reads stdout. See the
// "status" subcommand in main.go for the CLI entrypoint that calls this.
package status

import (
	"encoding/json"
	"fmt"

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

// jsonOutput matches the shape waybar's "custom" module expects.
type jsonOutput struct {
	Text    string `json:"text"`
	Tooltip string `json:"tooltip"`
	Class   string `json:"class"`
}

// FormatJSON renders a waybar-compatible JSON line. e is nil when no timer
// is running.
func FormatJSON(e *model.Entry) string {
	out := jsonOutput{Tooltip: "No timer running", Class: "idle"}
	if e != nil {
		out.Text = FormatText(e)
		out.Class = "running"
		tooltip := e.Task
		if e.Project != nil && e.Project.Client != nil {
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
