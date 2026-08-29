package status

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/aliasproject/notch/internal/model"
)

func mkRunningEntry() *model.Entry {
	client := &model.Client{ID: 1, Name: "Acme", HourlyRate: 100}
	project := &model.Project{ID: 1, ClientID: 1, Name: "Website", Client: client}
	start := time.Now().Add(-90 * time.Minute)
	return &model.Entry{ID: 1, ProjectID: 1, Project: project, Task: "Build feature", StartTime: start}
}

// ── FormatText ───────────────────────────────────────────────────────────────

func TestFormatText_Running(t *testing.T) {
	got := FormatText(mkRunningEntry())
	if !strings.Contains(got, "Build feature") {
		t.Errorf("FormatText = %q, want it to contain the task name", got)
	}
	if !strings.Contains(got, "1:30:0") { // ~1h30m, allow a second or two of test-run drift
		t.Errorf("FormatText = %q, want it to contain a ~1:30:0x duration", got)
	}
}

func TestFormatText_Idle(t *testing.T) {
	if got := FormatText(nil); got != "" {
		t.Errorf("FormatText(nil) = %q, want empty string so idle bars show nothing", got)
	}
}

// ── FormatJSON ───────────────────────────────────────────────────────────────

func TestFormatJSON_Running(t *testing.T) {
	raw := FormatJSON(mkRunningEntry())

	var out struct {
		Text    string `json:"text"`
		Tooltip string `json:"tooltip"`
		Class   string `json:"class"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("FormatJSON output is not valid JSON: %v\nraw: %s", err, raw)
	}

	if !strings.Contains(out.Text, "Build feature") {
		t.Errorf("text = %q, want it to contain the task name", out.Text)
	}
	if out.Class != "running" {
		t.Errorf("class = %q, want %q", out.Class, "running")
	}
	if !strings.Contains(out.Tooltip, "Acme") || !strings.Contains(out.Tooltip, "Website") || !strings.Contains(out.Tooltip, "Build feature") {
		t.Errorf("tooltip = %q, want it to mention the client, project, and task", out.Tooltip)
	}
}

func TestFormatJSON_Idle(t *testing.T) {
	raw := FormatJSON(nil)

	var out struct {
		Text    string `json:"text"`
		Tooltip string `json:"tooltip"`
		Class   string `json:"class"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("FormatJSON(nil) output is not valid JSON: %v\nraw: %s", err, raw)
	}

	if out.Text != "" {
		t.Errorf("text = %q, want empty for idle", out.Text)
	}
	if out.Class != "idle" {
		t.Errorf("class = %q, want %q", out.Class, "idle")
	}
	if out.Tooltip == "" {
		t.Error("tooltip should explain the idle state, not be empty")
	}
}

func TestFormatJSON_MissingProjectClientFallsBackToTaskOnlyTooltip(t *testing.T) {
	// GetRunningEntry always JOINs project+client, so this shouldn't happen in
	// practice, but FormatJSON shouldn't panic on a nil Project either.
	e := &model.Entry{ID: 1, Task: "Orphaned entry", StartTime: time.Now()}
	raw := FormatJSON(e)

	var out struct {
		Tooltip string `json:"tooltip"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("FormatJSON output is not valid JSON: %v\nraw: %s", err, raw)
	}
	if out.Tooltip != "Orphaned entry" {
		t.Errorf("tooltip = %q, want it to fall back to just the task name", out.Tooltip)
	}
}
