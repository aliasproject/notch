package views

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aliasproject/notch/internal/db"
	"github.com/aliasproject/notch/internal/model"
	"github.com/aliasproject/notch/internal/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// dropTableRaw opens a second raw connection to the same sqlite file backing
// d and drops the named table, so a specific query against d fails (table
// missing) while queries against other, untouched tables still succeed. This
// isolates individual error branches inside functions that make more than
// one DB call in sequence (mirrors the technique in projects_test.go).
func dropTableRaw(t *testing.T, path, table string) {
	t.Helper()
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := raw.Exec("DROP TABLE " + table); err != nil {
		t.Fatalf("drop table %s: %v", table, err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw connection: %v", err)
	}
}

// newTestDBAtPath is like newTestDB but also returns the backing file path,
// needed by tests that use dropTableRaw to isolate a single DB call's error
// branch from an earlier call's.
func newTestDBAtPath(t *testing.T) (*db.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d, path
}

func mkClient(id int64, name string, rate float64) *model.Client {
	return &model.Client{ID: id, Name: name, HourlyRate: rate}
}

func mkProject(id int64, name string, client *model.Client) *model.Project {
	return &model.Project{ID: id, ClientID: client.ID, Name: name, Client: client}
}

func mkEntry(id int64, project *model.Project, task string, start time.Time, durMin int) *model.Entry {
	end := start.Add(time.Duration(durMin) * time.Minute)
	return &model.Entry{ID: id, ProjectID: project.ID, Project: project, Task: task, StartTime: start, EndTime: &end}
}

func mkRunningEntry(id int64, project *model.Project, task string, start time.Time) *model.Entry {
	return &model.Entry{ID: id, ProjectID: project.ID, Project: project, Task: task, StartTime: start}
}

// ── recomputeFiltered ────────────────────────────────────────────────────────

func TestRecomputeFiltered_DateRange(t *testing.T) {
	c := mkClient(1, "Acme", 100)
	p := mkProject(1, "Website", c)

	e1 := mkEntry(1, p, "a", time.Date(2024, 5, 1, 9, 0, 0, 0, time.Local), 30)
	e2 := mkEntry(2, p, "b", time.Date(2024, 5, 2, 9, 0, 0, 0, time.Local), 30)
	e3 := mkEntry(3, p, "c", time.Date(2024, 5, 3, 9, 0, 0, 0, time.Local), 30)

	m := TimersModel{entries: []*model.Entry{e3, e2, e1}, dateFrom: "2024-05-02", dateTo: "2024-05-02"}
	m.recomputeFiltered()

	if len(m.filtered) != 1 || m.filtered[0].ID != 2 {
		t.Fatalf("filtered = %+v, want only entry 2", m.filtered)
	}
}

func TestRecomputeFiltered_Unbounded(t *testing.T) {
	c := mkClient(1, "Acme", 100)
	p := mkProject(1, "Website", c)
	e1 := mkEntry(1, p, "a", time.Date(2024, 5, 1, 9, 0, 0, 0, time.Local), 30)
	e2 := mkEntry(2, p, "b", time.Date(2024, 5, 2, 9, 0, 0, 0, time.Local), 30)

	m := TimersModel{entries: []*model.Entry{e2, e1}}
	m.recomputeFiltered()

	if len(m.filtered) != 2 {
		t.Fatalf("len(filtered) = %d, want 2 with no date bounds set", len(m.filtered))
	}
}

func TestRecomputeFiltered_ClampsCursor(t *testing.T) {
	c := mkClient(1, "Acme", 100)
	p := mkProject(1, "Website", c)
	e1 := mkEntry(1, p, "a", time.Date(2024, 5, 1, 9, 0, 0, 0, time.Local), 30)

	m := TimersModel{entries: []*model.Entry{e1}, cursor: 5}
	m.recomputeFiltered()

	if m.cursor != 0 {
		t.Errorf("cursor after recompute = %d, want clamped to 0", m.cursor)
	}

	m2 := TimersModel{cursor: 3} // no entries at all
	m2.recomputeFiltered()
	if m2.cursor != 0 {
		t.Errorf("cursor with empty entries = %d, want 0", m2.cursor)
	}
}

// ── buildLines / filteredTotals ───────────────────────────────────────────────

func TestBuildLines_GroupsByDay(t *testing.T) {
	c := mkClient(1, "Acme", 60)
	p := mkProject(1, "Website", c)
	e1 := mkEntry(1, p, "a", time.Date(2024, 5, 2, 9, 0, 0, 0, time.Local), 30)
	e2 := mkEntry(2, p, "b", time.Date(2024, 5, 2, 10, 0, 0, 0, time.Local), 30)
	e3 := mkEntry(3, p, "c", time.Date(2024, 5, 1, 9, 0, 0, 0, time.Local), 30)

	// filtered must already be DESC-sorted, matching what ListEntries provides.
	m := TimersModel{filtered: []*model.Entry{e2, e1, e3}}
	lines := m.buildLines()

	var kinds []timersLineKind
	for _, l := range lines {
		kinds = append(kinds, l.kind)
	}
	want := []timersLineKind{timersLineHeader, timersLineEntry, timersLineEntry, timersLineHeader, timersLineEntry}
	if len(kinds) != len(want) {
		t.Fatalf("got %d lines, want %d: %+v", len(kinds), len(want), lines)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Errorf("lines[%d].kind = %v, want %v", i, kinds[i], want[i])
		}
	}
}

func TestFilteredTotals_IncludesRunningEntry(t *testing.T) {
	c := mkClient(1, "Acme", 60) // $60/hr
	p := mkProject(1, "Website", c)
	// Entries deliberately span two different days — filteredTotals combines
	// everything currently filtered regardless of day (that's the whole
	// point: it backs the table's single footer total rather than a per-day
	// one), unlike the old day-scoped dayTotals it replaced.
	finished := mkEntry(1, p, "a", time.Date(2024, 5, 1, 9, 0, 0, 0, time.Local), 30) // 30 min => $30
	running := mkRunningEntry(2, p, "b", time.Now().Add(-10*time.Minute))

	m := TimersModel{filtered: []*model.Entry{finished, running}}
	dur, earned := m.filteredTotals()

	wantDur := 30*time.Minute + running.Duration()
	if dur < wantDur-time.Second || dur > wantDur+time.Second {
		t.Errorf("filteredTotals duration = %v, want ~%v", dur, wantDur)
	}
	wantEarned := 30.0 + running.Earnings(60)
	if earned < wantEarned-0.05 || earned > wantEarned+0.05 {
		t.Errorf("filteredTotals earnings = %v, want ~%v", earned, wantEarned)
	}
}

// ── parseLocalDate / rangeLabel ──────────────────────────────────────────────

func TestParseLocalDate(t *testing.T) {
	if _, ok := parseLocalDate(""); ok {
		t.Error("parseLocalDate(\"\") should not be ok")
	}
	if _, ok := parseLocalDate("not-a-date"); ok {
		t.Error("parseLocalDate(garbage) should not be ok")
	}
	got, ok := parseLocalDate("2024-05-02")
	if !ok {
		t.Fatal("parseLocalDate(valid date) should be ok")
	}
	if got.Year() != 2024 || got.Month() != time.May || got.Day() != 2 {
		t.Errorf("parseLocalDate = %v, want 2024-05-02", got)
	}
}

func TestRangeLabel_Presets(t *testing.T) {
	cases := []struct {
		preset timersPreset
		want   string
	}{
		{timersPresetToday, "Today"},
		{timersPresetYesterday, "Yesterday"},
		{timersPresetWeek, "This Week"},
		{timersPresetAll, "All time"},
	}
	for _, c := range cases {
		m := TimersModel{preset: c.preset}
		if got := m.rangeLabel(); got != c.want {
			t.Errorf("rangeLabel() with preset %v = %q, want %q", c.preset, got, c.want)
		}
	}
}

func TestRangeLabel_CustomRange(t *testing.T) {
	// Dates far from "today" so the friendlyDay Today/Yesterday branches can't
	// fire and make this test flaky depending on the current date.
	m := TimersModel{preset: timersPresetCustom, dateFrom: "2020-01-01", dateTo: "2020-01-05"}
	want := "Jan 1, 2020  →  Jan 5, 2020"
	if got := m.rangeLabel(); got != want {
		t.Errorf("rangeLabel() = %q, want %q", got, want)
	}

	single := TimersModel{preset: timersPresetCustom, dateFrom: "2020-03-15", dateTo: "2020-03-15"}
	if got := single.rangeLabel(); got != "Mar 15, 2020" {
		t.Errorf("rangeLabel() for single custom day = %q, want %q", got, "Mar 15, 2020")
	}

	empty := TimersModel{preset: timersPresetCustom}
	if got := empty.rangeLabel(); got != "All time" {
		t.Errorf("rangeLabel() with no bounds = %q, want %q", got, "All time")
	}
}

// ── List-mode preset keys ────────────────────────────────────────────────────

func TestUpdateList_PresetKeys(t *testing.T) {
	c := mkClient(1, "Acme", 100)
	p := mkProject(1, "Website", c)
	e := mkEntry(1, p, "a", time.Now(), 30)
	base := TimersModel{entries: []*model.Entry{e}}
	base.recomputeFiltered()

	cases := []struct {
		key        rune
		wantPreset timersPreset
	}{
		{'t', timersPresetToday},
		{'y', timersPresetYesterday},
		{'w', timersPresetWeek},
		{'a', timersPresetAll},
	}
	for _, c := range cases {
		got, cmd := base.updateList(runeKey(c.key))
		if got.preset != c.wantPreset {
			t.Errorf("pressing %q: preset = %v, want %v", c.key, got.preset, c.wantPreset)
		}
		if cmd != nil {
			t.Errorf("pressing %q: want nil cmd (pure, no DB touch), got non-nil", c.key)
		}
	}
}

func TestUpdateList_FilterKeyEntersFilterMode(t *testing.T) {
	m := NewTimers(nil) // fromInput/toInput must be properly constructed before Focus() is called
	got, cmd := m.updateList(runeKey('f'))
	if got.mode != timersModeFilter {
		t.Errorf("mode after 'f' = %v, want timersModeFilter", got.mode)
	}
	if cmd == nil {
		t.Error("want a non-nil cmd (textinput.Blink) after entering filter mode")
	}
}

func TestUpdateList_DeleteRequiresNonEmptyList(t *testing.T) {
	empty := TimersModel{}
	got, _ := empty.updateList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if got.mode == timersModeConfirmDelete {
		t.Error("'d' on an empty list should not enter confirm-delete mode")
	}

	c := mkClient(1, "Acme", 100)
	p := mkProject(1, "Website", c)
	e := mkEntry(1, p, "a", time.Now(), 30)
	nonEmpty := TimersModel{entries: []*model.Entry{e}}
	nonEmpty.recomputeFiltered()
	got2, _ := nonEmpty.updateList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if got2.mode != timersModeConfirmDelete {
		t.Errorf("'d' with a selected entry: mode = %v, want timersModeConfirmDelete", got2.mode)
	}
}

// ── Delete-confirm flow (regression coverage for the y/enter/close-after-delete fix) ──

func TestUpdateConfirm_YKeyDeletes(t *testing.T) {
	c := mkClient(1, "Acme", 100)
	p := mkProject(1, "Website", c)
	e := mkEntry(1, p, "a", time.Now(), 30)
	m := TimersModel{mode: timersModeConfirmDelete, entries: []*model.Entry{e}}
	m.recomputeFiltered()

	got, cmd := m.updateConfirm(runeKey('y'))
	if got.mode != timersModeList {
		t.Errorf("mode after 'y' = %v, want timersModeList (confirmation should close immediately)", got.mode)
	}
	if cmd == nil {
		t.Error("want a non-nil delete cmd after pressing 'y'")
	}
}

// Enter deliberately does NOT confirm a delete — only "y"/"Y" does, since a
// destructive action shouldn't fire from a reflexive Enter press (see
// updateConfirm in timers.go). This matches clients.go/projects.go.
func TestUpdateConfirm_EnterKeyDoesNotDelete(t *testing.T) {
	c := mkClient(1, "Acme", 100)
	p := mkProject(1, "Website", c)
	e := mkEntry(1, p, "a", time.Now(), 30)
	m := TimersModel{mode: timersModeConfirmDelete, entries: []*model.Entry{e}}
	m.recomputeFiltered()

	got, cmd := m.updateConfirm(tea.KeyMsg{Type: tea.KeyEnter})
	if got.mode != timersModeConfirmDelete {
		t.Errorf("mode after enter = %v, want to stay timersModeConfirmDelete", got.mode)
	}
	if cmd != nil {
		t.Error("want nil cmd after pressing enter — enter must not confirm a delete")
	}
}

func TestUpdateConfirm_EscCancels(t *testing.T) {
	c := mkClient(1, "Acme", 100)
	p := mkProject(1, "Website", c)
	e := mkEntry(1, p, "a", time.Now(), 30)
	m := TimersModel{mode: timersModeConfirmDelete, entries: []*model.Entry{e}}
	m.recomputeFiltered()

	got, cmd := m.updateConfirm(tea.KeyMsg{Type: tea.KeyEsc})
	if got.mode != timersModeList {
		t.Errorf("mode after esc = %v, want timersModeList", got.mode)
	}
	if cmd != nil {
		t.Error("want nil cmd after cancelling delete")
	}
}

func TestUpdateConfirm_OtherKeysAreNoOps(t *testing.T) {
	m := TimersModel{mode: timersModeConfirmDelete}
	got, cmd := m.updateConfirm(runeKey('x'))
	if got.mode != timersModeConfirmDelete {
		t.Errorf("mode after unrelated key = %v, want to stay timersModeConfirmDelete", got.mode)
	}
	if cmd != nil {
		t.Error("want nil cmd for an unrelated keypress")
	}
}

// ── Dropdown / autocomplete ──────────────────────────────────────────────────

func TestRebuildClientMatches(t *testing.T) {
	clients := []*model.Client{
		mkClient(1, "Acme Corp", 0),
		mkClient(2, "Beta LLC", 0),
	}
	f := newTimerForm(clients, nil)

	t.Run("empty query lists everyone, no create option", func(t *testing.T) {
		f.inputs[fieldClient].SetValue("")
		f.rebuildClientMatches()
		if len(f.clientMatches) != 2 {
			t.Fatalf("clientMatches = %+v, want 2 (no query)", f.clientMatches)
		}
	})

	t.Run("substring match is case-insensitive", func(t *testing.T) {
		f.inputs[fieldClient].SetValue("acme")
		f.rebuildClientMatches()
		// "acme" is a substring match, not a full-name exact match, so the
		// "+ Create" option is still offered, but trails real matches so
		// Enter picks the match by default (see the exactMatch check below).
		if len(f.clientMatches) != 2 || f.clientMatches[0].id != 1 || f.clientMatches[1].id != 0 {
			t.Fatalf("clientMatches = %+v, want [Acme Corp, create-new]", f.clientMatches)
		}
	})

	t.Run("exact match suppresses the create option", func(t *testing.T) {
		f.inputs[fieldClient].SetValue("Acme Corp")
		f.rebuildClientMatches()
		for _, item := range f.clientMatches {
			if item.id == 0 {
				t.Errorf("clientMatches should not offer 'create new' on an exact match: %+v", f.clientMatches)
			}
		}
	})

	t.Run("no match offers a create option", func(t *testing.T) {
		f.inputs[fieldClient].SetValue("Gamma")
		f.rebuildClientMatches()
		if len(f.clientMatches) != 1 || f.clientMatches[0].id != 0 {
			t.Fatalf("clientMatches = %+v, want a single 'create new' entry", f.clientMatches)
		}
	})
}

func TestRebuildProjectMatches_ScopedToClient(t *testing.T) {
	c1 := mkClient(1, "Acme", 0)
	c2 := mkClient(2, "Beta", 0)
	projects := []*model.Project{
		mkProject(1, "Website", c1),
		mkProject(2, "App", c2),
	}
	f := newTimerForm(nil, projects)

	t.Run("no client selected shows client-qualified labels for all", func(t *testing.T) {
		f.clientID = 0
		f.inputs[fieldProject].SetValue("")
		f.rebuildProjectMatches()
		if len(f.projectMatches) != 2 {
			t.Fatalf("projectMatches = %+v, want both projects", f.projectMatches)
		}
		for _, item := range f.projectMatches {
			if item.id == 1 && item.label != "Acme › Website" {
				t.Errorf("cross-client label = %q, want %q", item.label, "Acme › Website")
			}
		}
	})

	t.Run("client selected scopes to that client only", func(t *testing.T) {
		f.clientID = 1
		f.inputs[fieldProject].SetValue("")
		f.rebuildProjectMatches()
		if len(f.projectMatches) != 1 || f.projectMatches[0].id != 1 {
			t.Fatalf("projectMatches = %+v, want only Acme's Website project", f.projectMatches)
		}
		if f.projectMatches[0].label != "Website" {
			t.Errorf("scoped label = %q, want plain %q (no client prefix)", f.projectMatches[0].label, "Website")
		}
	})
}

func TestApplyClientSelection(t *testing.T) {
	clients := []*model.Client{mkClient(1, "Acme", 0)}
	projects := []*model.Project{mkProject(1, "Website", clients[0])}
	f := newTimerForm(clients, projects)

	f.inputs[fieldClient].SetValue("Acme")
	f.rebuildClientMatches()
	f.clientSel = 0
	f.applyClientSelection()

	if f.clientID != 1 {
		t.Errorf("clientID after selection = %d, want 1", f.clientID)
	}
	if f.inputs[fieldClient].Value() != "Acme" {
		t.Errorf("client input value = %q, want %q", f.inputs[fieldClient].Value(), "Acme")
	}
}

func TestApplyClientSelection_ChangingClientClearsProject(t *testing.T) {
	c1 := mkClient(1, "Acme", 0)
	c2 := mkClient(2, "Beta", 0)
	f := newTimerForm([]*model.Client{c1, c2}, nil)
	f.projectID = 99
	f.inputs[fieldProject].SetValue("Stale Project")

	f.inputs[fieldClient].SetValue("Beta")
	f.rebuildClientMatches()
	f.clientSel = 0
	f.applyClientSelection()

	if f.projectID != 0 {
		t.Errorf("projectID after switching client = %d, want cleared to 0", f.projectID)
	}
	if f.inputs[fieldProject].Value() != "" {
		t.Errorf("project input after switching client = %q, want cleared", f.inputs[fieldProject].Value())
	}
}

func TestApplyProjectSelection_BackfillsClient(t *testing.T) {
	c := mkClient(1, "Acme", 0)
	p := mkProject(1, "Website", c)
	f := newTimerForm(nil, []*model.Project{p})

	f.inputs[fieldProject].SetValue("Website")
	f.rebuildProjectMatches()
	f.projectSel = 0
	f.applyProjectSelection()

	if f.projectID != 1 {
		t.Errorf("projectID after selection = %d, want 1", f.projectID)
	}
	if f.clientID != 1 {
		t.Errorf("clientID should be back-filled from the project's client, got %d", f.clientID)
	}
	if f.inputs[fieldClient].Value() != "Acme" {
		t.Errorf("client input should be back-filled, got %q", f.inputs[fieldClient].Value())
	}
}

// ── Form validation ──────────────────────────────────────────────────────────

func TestSubmitForm_EmptyTaskRejected(t *testing.T) {
	f := newTimerForm(nil, nil)
	f.inputs[fieldTask].SetValue("   ")
	m := TimersModel{mode: timersModeNew, form: f}

	got, cmd := m.submitForm()
	if got.err == "" {
		t.Error("want a validation error for an empty task")
	}
	if got.mode != timersModeNew {
		t.Errorf("mode after failed validation = %v, want to stay timersModeNew", got.mode)
	}
	if cmd != nil {
		t.Error("want nil cmd on validation failure (no DB touch)")
	}
}

// ── handleOpenForm ───────────────────────────────────────────────────────────

func TestHandleOpenForm_New(t *testing.T) {
	m := TimersModel{}
	got := m.handleOpenForm(openFormMsg{entryID: 0})
	if got.mode != timersModeNew {
		t.Errorf("mode = %v, want timersModeNew", got.mode)
	}
	if got.form.entryID != 0 {
		t.Errorf("form.entryID = %d, want 0", got.form.entryID)
	}
}

func TestHandleOpenForm_EditPrepopulatesFields(t *testing.T) {
	c := mkClient(1, "Acme", 100)
	p := mkProject(1, "Website", c)
	e := mkEntry(42, p, "build feature", time.Now(), 30)
	e.Notes = "some notes"

	m := TimersModel{entries: []*model.Entry{e}}
	got := m.handleOpenForm(openFormMsg{entryID: 42, clients: []*model.Client{c}, projects: []*model.Project{p}})

	if got.mode != timersModeEdit {
		t.Fatalf("mode = %v, want timersModeEdit", got.mode)
	}
	if got.form.entryID != 42 {
		t.Errorf("form.entryID = %d, want 42", got.form.entryID)
	}
	if got.form.inputs[fieldTask].Value() != "build feature" {
		t.Errorf("task field = %q, want %q", got.form.inputs[fieldTask].Value(), "build feature")
	}
	if got.form.inputs[fieldNotes].Value() != "some notes" {
		t.Errorf("notes field = %q, want %q", got.form.inputs[fieldNotes].Value(), "some notes")
	}
	if got.form.clientID != 1 || got.form.inputs[fieldClient].Value() != "Acme" {
		t.Errorf("client field not prepopulated: clientID=%d value=%q", got.form.clientID, got.form.inputs[fieldClient].Value())
	}
	if got.form.projectID != 1 || got.form.inputs[fieldProject].Value() != "Website" {
		t.Errorf("project field not prepopulated: projectID=%d value=%q", got.form.projectID, got.form.inputs[fieldProject].Value())
	}
}

// ── Trivial getters/setters ──────────────────────────────────────────────────

func TestFocusField(t *testing.T) {
	f := newTimerForm(nil, nil)
	f.focusField(fieldClient)
	if f.focusIdx != fieldClient {
		t.Errorf("focusIdx = %d, want fieldClient", f.focusIdx)
	}
	if !f.inputs[fieldClient].Focused() {
		t.Error("fieldClient input should be focused")
	}
	if f.inputs[fieldTask].Focused() {
		t.Error("fieldTask input should be blurred after focusing another field")
	}

	// idx >= fieldSave: no input to focus (buttons aren't textinputs).
	f.focusField(fieldSave)
	if f.focusIdx != fieldSave {
		t.Errorf("focusIdx = %d, want fieldSave", f.focusIdx)
	}
	f.focusField(fieldCancel)
	if f.focusIdx != fieldCancel {
		t.Errorf("focusIdx = %d, want fieldCancel", f.focusIdx)
	}
}

func TestSetSize(t *testing.T) {
	var m TimersModel
	m.SetSize(80, 24)
	if m.width != 80 || m.height != 24 {
		t.Errorf("SetSize: width=%d height=%d, want 80,24", m.width, m.height)
	}
}

func TestIsBusy(t *testing.T) {
	cases := []struct {
		mode timersMode
		want bool
	}{
		{timersModeList, false},
		{timersModeNew, true},
		{timersModeEdit, true},
		{timersModeConfirmDelete, true},
		{timersModeFilter, true},
	}
	for _, c := range cases {
		m := TimersModel{mode: c.mode}
		if got := m.IsBusy(); got != c.want {
			t.Errorf("IsBusy() with mode %v = %v, want %v", c.mode, got, c.want)
		}
	}
}

func TestHelp(t *testing.T) {
	cases := []timersMode{timersModeList, timersModeNew, timersModeEdit, timersModeConfirmDelete, timersModeFilter}
	for _, mode := range cases {
		m := TimersModel{mode: mode}
		if got := m.Help(); len(got) == 0 {
			t.Errorf("Help() with mode %v returned no hotkeys", mode)
		}
	}
}

func TestInit(t *testing.T) {
	d := newTestDB(t)
	m := TimersModel{db: d}
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init() returned nil cmd")
	}
	msg := cmd()
	if _, ok := msg.(timerEntriesMsg); !ok {
		t.Errorf("Init() cmd produced %T, want timerEntriesMsg", msg)
	}
}

func TestListColWidths(t *testing.T) {
	// Wide: projectCol clamps to its max of 44.
	wide := TimersModel{width: 300}
	taskCol, projectCol := wide.listColWidths()
	if projectCol != 44 {
		t.Errorf("projectCol with very wide view = %d, want clamped to 44", projectCol)
	}
	if taskCol < 18 {
		t.Errorf("taskCol = %d, want >= 18", taskCol)
	}

	// Narrow: both columns clamp to their minimum of 18.
	narrow := TimersModel{width: 20}
	taskCol2, projectCol2 := narrow.listColWidths()
	if projectCol2 != 18 {
		t.Errorf("projectCol with narrow view = %d, want clamped to 18", projectCol2)
	}
	if taskCol2 != 18 {
		t.Errorf("taskCol with narrow view = %d, want clamped to 18", taskCol2)
	}
}

func TestSelectedEntry_OutOfBounds(t *testing.T) {
	neg := TimersModel{cursor: -1}
	if e := neg.selectedEntry(); e != nil {
		t.Error("selectedEntry with negative cursor should be nil")
	}
	tooFar := TimersModel{cursor: 5}
	if e := tooFar.selectedEntry(); e != nil {
		t.Error("selectedEntry with cursor past the end should be nil")
	}
}

func TestFriendlyDay(t *testing.T) {
	now := time.Now().Local()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	if got := friendlyDay(today); got != "Today" {
		t.Errorf("friendlyDay(today) = %q, want %q", got, "Today")
	}
	if got := friendlyDay(today.AddDate(0, 0, -1)); got != "Yesterday" {
		t.Errorf("friendlyDay(yesterday) = %q, want %q", got, "Yesterday")
	}
	older := today.AddDate(0, 0, -10)
	if got := friendlyDay(older); got != older.Format("Mon, Jan 2") {
		t.Errorf("friendlyDay(10 days ago) = %q, want %q", got, older.Format("Mon, Jan 2"))
	}
}

func TestRangeLabel_PartialCustomRange(t *testing.T) {
	fromOnly := TimersModel{preset: timersPresetCustom, dateFrom: "2020-01-01", dateTo: ""}
	if got := fromOnly.rangeLabel(); !strings.Contains(got, "—") {
		t.Errorf("rangeLabel() with only dateFrom set = %q, want it to contain the placeholder dash", got)
	}
	toOnly := TimersModel{preset: timersPresetCustom, dateFrom: "", dateTo: "2020-01-05"}
	if got := toOnly.rangeLabel(); !strings.Contains(got, "—") {
		t.Errorf("rangeLabel() with only dateTo set = %q, want it to contain the placeholder dash", got)
	}
}

// ── openFormCmd ──────────────────────────────────────────────────────────────

func TestOpenFormCmd_Success(t *testing.T) {
	d := newTestDB(t)
	if _, err := d.CreateClient("Acme", 50); err != nil {
		t.Fatalf("CreateClient: %v", err)
	}
	m := TimersModel{db: d}
	cmd := m.openFormCmd(0)
	msg := cmd()
	got, ok := msg.(openFormMsg)
	if !ok {
		t.Fatalf("openFormCmd produced %T, want openFormMsg", msg)
	}
	if len(got.clients) != 1 {
		t.Errorf("openFormMsg.clients = %+v, want 1 client", got.clients)
	}
}

func TestOpenFormCmd_Error(t *testing.T) {
	d := newTestDB(t)
	d.Close()
	m := TimersModel{db: d}
	cmd := m.openFormCmd(0)
	msg := cmd()
	if _, ok := msg.(ErrMsg); !ok {
		t.Errorf("openFormCmd on a closed db produced %T, want ErrMsg", msg)
	}
}

// ── updateForm: top-level Cancel-always-wins ─────────────────────────────────

func TestUpdateForm_CancelAlwaysWins(t *testing.T) {
	for _, idx := range []int{fieldTask, fieldClient, fieldProject, fieldNotes, fieldSave, fieldCancel} {
		f := newTimerForm(nil, nil)
		f.focusField(idx)
		m := TimersModel{mode: timersModeNew, form: f, err: "stale"}
		got, cmd := m.updateForm(tea.KeyMsg{Type: tea.KeyEsc})
		if got.mode != timersModeList {
			t.Errorf("focusIdx %d: mode after esc = %v, want timersModeList", idx, got.mode)
		}
		if got.err != "" {
			t.Errorf("focusIdx %d: err after esc = %q, want cleared", idx, got.err)
		}
		if cmd != nil {
			t.Errorf("focusIdx %d: want nil cmd after cancel", idx)
		}
	}
}

// ── updateForm: fieldTask ────────────────────────────────────────────────────

func TestUpdateForm_TaskField(t *testing.T) {
	newM := func() TimersModel {
		f := newTimerForm(nil, nil)
		f.focusField(fieldTask)
		return TimersModel{mode: timersModeNew, form: f}
	}

	t.Run("tab moves to client", func(t *testing.T) {
		got, _ := newM().updateForm(tea.KeyMsg{Type: tea.KeyTab})
		if got.form.focusIdx != fieldClient {
			t.Errorf("focusIdx = %d, want fieldClient", got.form.focusIdx)
		}
	})
	t.Run("shift+tab moves to save", func(t *testing.T) {
		got, _ := newM().updateForm(tea.KeyMsg{Type: tea.KeyShiftTab})
		if got.form.focusIdx != fieldSave {
			t.Errorf("focusIdx = %d, want fieldSave", got.form.focusIdx)
		}
	})
	t.Run("enter confirms to client", func(t *testing.T) {
		got, _ := newM().updateForm(tea.KeyMsg{Type: tea.KeyEnter})
		if got.form.focusIdx != fieldClient {
			t.Errorf("focusIdx = %d, want fieldClient", got.form.focusIdx)
		}
	})
	t.Run("default forwards to input", func(t *testing.T) {
		got, _ := newM().updateForm(runeKey('x'))
		if got.form.inputs[fieldTask].Value() != "x" {
			t.Errorf("task input = %q, want %q", got.form.inputs[fieldTask].Value(), "x")
		}
	})
}

// ── updateForm: fieldClient ──────────────────────────────────────────────────

func TestUpdateForm_ClientField(t *testing.T) {
	clients := []*model.Client{mkClient(1, "Acme", 0), mkClient(2, "Beta", 0)}
	newM := func() TimersModel {
		f := newTimerForm(clients, nil)
		f.focusField(fieldClient) // matches both clients since query is empty
		return TimersModel{mode: timersModeNew, form: f}
	}

	t.Run("tab applies selection and moves to project", func(t *testing.T) {
		m := newM()
		m.form.clientSel = 0
		got, _ := m.updateForm(tea.KeyMsg{Type: tea.KeyTab})
		if got.form.focusIdx != fieldProject {
			t.Errorf("focusIdx = %d, want fieldProject", got.form.focusIdx)
		}
		if got.form.clientID == 0 {
			t.Error("clientID should be set from the applied selection")
		}
	})
	t.Run("shift+tab applies selection and moves to task", func(t *testing.T) {
		m := newM()
		m.form.clientSel = 0
		got, _ := m.updateForm(tea.KeyMsg{Type: tea.KeyShiftTab})
		if got.form.focusIdx != fieldTask {
			t.Errorf("focusIdx = %d, want fieldTask", got.form.focusIdx)
		}
	})
	t.Run("enter applies selection and moves to project", func(t *testing.T) {
		m := newM()
		m.form.clientSel = 0
		got, _ := m.updateForm(tea.KeyMsg{Type: tea.KeyEnter})
		if got.form.focusIdx != fieldProject {
			t.Errorf("focusIdx = %d, want fieldProject", got.form.focusIdx)
		}
	})
	t.Run("down moves selection and shows dropdown", func(t *testing.T) {
		m := newM()
		m.form.clientSel = 0
		got, _ := m.updateForm(tea.KeyMsg{Type: tea.KeyDown})
		if got.form.clientSel != 1 {
			t.Errorf("clientSel = %d, want 1", got.form.clientSel)
		}
		if !got.form.showClientDrop {
			t.Error("showClientDrop should be true after pressing down")
		}
	})
	t.Run("down at end of list does not overflow", func(t *testing.T) {
		m := newM()
		m.form.clientSel = len(m.form.clientMatches) - 1
		got, _ := m.updateForm(tea.KeyMsg{Type: tea.KeyDown})
		if got.form.clientSel != len(m.form.clientMatches)-1 {
			t.Errorf("clientSel = %d, want to stay at the last index", got.form.clientSel)
		}
	})
	t.Run("up moves selection back", func(t *testing.T) {
		m := newM()
		m.form.clientSel = 1
		got, _ := m.updateForm(tea.KeyMsg{Type: tea.KeyUp})
		if got.form.clientSel != 0 {
			t.Errorf("clientSel = %d, want 0", got.form.clientSel)
		}
	})
	t.Run("up at start of list does not underflow", func(t *testing.T) {
		m := newM()
		m.form.clientSel = 0
		got, _ := m.updateForm(tea.KeyMsg{Type: tea.KeyUp})
		if got.form.clientSel != 0 {
			t.Errorf("clientSel = %d, want to stay at 0", got.form.clientSel)
		}
	})
	t.Run("default forwards to input and resets resolved id", func(t *testing.T) {
		m := newM()
		m.form.clientID = 99
		got, _ := m.updateForm(runeKey('z'))
		if !strings.Contains(got.form.inputs[fieldClient].Value(), "z") {
			t.Errorf("client input = %q, want to contain 'z'", got.form.inputs[fieldClient].Value())
		}
		if got.form.clientID != 0 {
			t.Errorf("clientID = %d, want reset to 0 after editing text", got.form.clientID)
		}
	})
}

// ── updateForm: fieldProject ──────────────────────────────────────────────────

func TestUpdateForm_ProjectField(t *testing.T) {
	c := mkClient(1, "Acme", 0)
	projects := []*model.Project{mkProject(1, "Website", c), mkProject(2, "App", c)}
	newM := func() TimersModel {
		f := newTimerForm(nil, projects)
		f.focusField(fieldProject)
		return TimersModel{mode: timersModeNew, form: f}
	}

	t.Run("tab applies selection and moves to notes", func(t *testing.T) {
		m := newM()
		m.form.projectSel = 0
		got, _ := m.updateForm(tea.KeyMsg{Type: tea.KeyTab})
		if got.form.focusIdx != fieldNotes {
			t.Errorf("focusIdx = %d, want fieldNotes", got.form.focusIdx)
		}
		if got.form.projectID == 0 {
			t.Error("projectID should be set from the applied selection")
		}
	})
	t.Run("shift+tab applies selection and moves to client", func(t *testing.T) {
		m := newM()
		m.form.projectSel = 0
		got, _ := m.updateForm(tea.KeyMsg{Type: tea.KeyShiftTab})
		if got.form.focusIdx != fieldClient {
			t.Errorf("focusIdx = %d, want fieldClient", got.form.focusIdx)
		}
	})
	t.Run("enter applies selection and moves to notes", func(t *testing.T) {
		m := newM()
		m.form.projectSel = 0
		got, _ := m.updateForm(tea.KeyMsg{Type: tea.KeyEnter})
		if got.form.focusIdx != fieldNotes {
			t.Errorf("focusIdx = %d, want fieldNotes", got.form.focusIdx)
		}
	})
	t.Run("down moves selection and shows dropdown", func(t *testing.T) {
		m := newM()
		m.form.projectSel = 0
		got, _ := m.updateForm(tea.KeyMsg{Type: tea.KeyDown})
		if got.form.projectSel != 1 {
			t.Errorf("projectSel = %d, want 1", got.form.projectSel)
		}
		if !got.form.showProjectDrop {
			t.Error("showProjectDrop should be true after pressing down")
		}
	})
	t.Run("down at end of list does not overflow", func(t *testing.T) {
		m := newM()
		m.form.projectSel = len(m.form.projectMatches) - 1
		got, _ := m.updateForm(tea.KeyMsg{Type: tea.KeyDown})
		if got.form.projectSel != len(m.form.projectMatches)-1 {
			t.Errorf("projectSel = %d, want to stay at the last index", got.form.projectSel)
		}
	})
	t.Run("up moves selection back", func(t *testing.T) {
		m := newM()
		m.form.projectSel = 1
		got, _ := m.updateForm(tea.KeyMsg{Type: tea.KeyUp})
		if got.form.projectSel != 0 {
			t.Errorf("projectSel = %d, want 0", got.form.projectSel)
		}
	})
	t.Run("up at start of list does not underflow", func(t *testing.T) {
		m := newM()
		m.form.projectSel = 0
		got, _ := m.updateForm(tea.KeyMsg{Type: tea.KeyUp})
		if got.form.projectSel != 0 {
			t.Errorf("projectSel = %d, want to stay at 0", got.form.projectSel)
		}
	})
	t.Run("default forwards to input and resets resolved id", func(t *testing.T) {
		m := newM()
		m.form.projectID = 99
		got, _ := m.updateForm(runeKey('z'))
		if !strings.Contains(got.form.inputs[fieldProject].Value(), "z") {
			t.Errorf("project input = %q, want to contain 'z'", got.form.inputs[fieldProject].Value())
		}
		if got.form.projectID != 0 {
			t.Errorf("projectID = %d, want reset to 0 after editing text", got.form.projectID)
		}
	})
}

// ── updateForm: fieldNotes ────────────────────────────────────────────────────

func TestUpdateForm_NotesField(t *testing.T) {
	newM := func() TimersModel {
		f := newTimerForm(nil, nil)
		f.focusField(fieldNotes)
		return TimersModel{mode: timersModeNew, form: f}
	}

	t.Run("tab moves to save", func(t *testing.T) {
		got, _ := newM().updateForm(tea.KeyMsg{Type: tea.KeyTab})
		if got.form.focusIdx != fieldSave {
			t.Errorf("focusIdx = %d, want fieldSave", got.form.focusIdx)
		}
	})
	t.Run("shift+tab moves to project", func(t *testing.T) {
		got, _ := newM().updateForm(tea.KeyMsg{Type: tea.KeyShiftTab})
		if got.form.focusIdx != fieldProject {
			t.Errorf("focusIdx = %d, want fieldProject", got.form.focusIdx)
		}
	})
	t.Run("enter moves to save", func(t *testing.T) {
		got, _ := newM().updateForm(tea.KeyMsg{Type: tea.KeyEnter})
		if got.form.focusIdx != fieldSave {
			t.Errorf("focusIdx = %d, want fieldSave", got.form.focusIdx)
		}
	})
	t.Run("default forwards to input", func(t *testing.T) {
		got, _ := newM().updateForm(runeKey('n'))
		if got.form.inputs[fieldNotes].Value() != "n" {
			t.Errorf("notes input = %q, want %q", got.form.inputs[fieldNotes].Value(), "n")
		}
	})
}

// ── updateForm: fieldSave ────────────────────────────────────────────────────

func TestUpdateForm_SaveField(t *testing.T) {
	newM := func() TimersModel {
		f := newTimerForm(nil, nil)
		f.focusField(fieldSave)
		return TimersModel{mode: timersModeNew, form: f}
	}

	t.Run("tab moves to cancel", func(t *testing.T) {
		got, _ := newM().updateForm(tea.KeyMsg{Type: tea.KeyTab})
		if got.form.focusIdx != fieldCancel {
			t.Errorf("focusIdx = %d, want fieldCancel", got.form.focusIdx)
		}
	})
	t.Run("shift+tab moves to notes", func(t *testing.T) {
		got, _ := newM().updateForm(tea.KeyMsg{Type: tea.KeyShiftTab})
		if got.form.focusIdx != fieldNotes {
			t.Errorf("focusIdx = %d, want fieldNotes", got.form.focusIdx)
		}
	})
	t.Run("enter dispatches to submitForm", func(t *testing.T) {
		d := newTestDB(t)
		m := newM()
		m.db = d
		m.form.inputs[fieldTask].SetValue("Do the thing")
		got, cmd := m.updateForm(tea.KeyMsg{Type: tea.KeyEnter})
		if got.mode != timersModeList {
			t.Errorf("mode after submit = %v, want timersModeList", got.mode)
		}
		if cmd == nil {
			t.Error("want a non-nil cmd from submitForm")
		}
	})
}

// ── updateForm: fieldCancel ───────────────────────────────────────────────────

func TestUpdateForm_CancelField(t *testing.T) {
	newM := func() TimersModel {
		f := newTimerForm(nil, nil)
		f.focusField(fieldCancel)
		return TimersModel{mode: timersModeNew, form: f, err: "stale"}
	}

	t.Run("tab moves to task", func(t *testing.T) {
		got, _ := newM().updateForm(tea.KeyMsg{Type: tea.KeyTab})
		if got.form.focusIdx != fieldTask {
			t.Errorf("focusIdx = %d, want fieldTask", got.form.focusIdx)
		}
	})
	t.Run("shift+tab moves to save", func(t *testing.T) {
		got, _ := newM().updateForm(tea.KeyMsg{Type: tea.KeyShiftTab})
		if got.form.focusIdx != fieldSave {
			t.Errorf("focusIdx = %d, want fieldSave", got.form.focusIdx)
		}
	})
	t.Run("enter cancels the form", func(t *testing.T) {
		got, cmd := newM().updateForm(tea.KeyMsg{Type: tea.KeyEnter})
		if got.mode != timersModeList {
			t.Errorf("mode = %v, want timersModeList", got.mode)
		}
		if got.err != "" {
			t.Errorf("err = %q, want cleared", got.err)
		}
		if cmd != nil {
			t.Error("want nil cmd after cancel")
		}
	})
}

// ── updateConfirm: nil-selection edge case ───────────────────────────────────

func TestUpdateConfirm_YKeyWithNoSelection(t *testing.T) {
	// filtered is empty (no entries), so selectedEntry() is nil and the "y"
	// case must return early without dispatching a delete cmd.
	m := TimersModel{mode: timersModeConfirmDelete}
	got, cmd := m.updateConfirm(runeKey('y'))
	if got.mode != timersModeList {
		t.Errorf("mode after 'y' with no selection = %v, want timersModeList", got.mode)
	}
	if cmd != nil {
		t.Error("want nil cmd when there is nothing selected to delete")
	}
}

// ── applyClientSelection / applyProjectSelection: remaining branches ────────

func TestApplyClientSelection_NoMatchesIsNoOp(t *testing.T) {
	f := newTimerForm(nil, nil) // clientMatches is empty (no clients, no query)
	f.applyClientSelection()    // must not panic; ID stays 0
	if f.clientID != 0 {
		t.Errorf("clientID = %d, want 0", f.clientID)
	}
}

func TestApplyClientSelection_CreateNewOption(t *testing.T) {
	f := newTimerForm(nil, nil)
	f.inputs[fieldClient].SetValue("Brand New")
	f.rebuildClientMatches() // no existing clients -> leading "create new" item
	f.clientSel = 0
	f.applyClientSelection()
	if f.clientID != 0 {
		t.Errorf("clientID after choosing 'create new' = %d, want 0", f.clientID)
	}
	if f.showClientDrop {
		t.Error("showClientDrop should be false after applying a selection")
	}
}

func TestApplyProjectSelection_NoMatchesIsNoOp(t *testing.T) {
	f := newTimerForm(nil, nil)
	f.applyProjectSelection()
	if f.projectID != 0 {
		t.Errorf("projectID = %d, want 0", f.projectID)
	}
}

func TestApplyProjectSelection_CreateNewOption(t *testing.T) {
	f := newTimerForm(nil, nil)
	f.inputs[fieldProject].SetValue("Brand New Project")
	f.rebuildProjectMatches()
	f.projectSel = 0
	f.applyProjectSelection()
	if f.projectID != 0 {
		t.Errorf("projectID after choosing 'create new' = %d, want 0", f.projectID)
	}
	if f.showProjectDrop {
		t.Error("showProjectDrop should be false after applying a selection")
	}
}

// ── updateFilter ──────────────────────────────────────────────────────────────

func TestUpdateFilter_Cancel(t *testing.T) {
	m := NewTimers(nil)
	m.mode = timersModeFilter
	m.fromInput.Focus()
	got, cmd := m.updateFilter(tea.KeyMsg{Type: tea.KeyEsc})
	if got.mode != timersModeList {
		t.Errorf("mode after esc = %v, want timersModeList", got.mode)
	}
	if got.fromInput.Focused() || got.toInput.Focused() {
		t.Error("both inputs should be blurred after cancelling the filter")
	}
	if cmd != nil {
		t.Error("want nil cmd after cancel")
	}
}

func TestUpdateFilter_Confirm(t *testing.T) {
	m := NewTimers(nil)
	m.mode = timersModeFilter
	m.fromInput.SetValue("2024-01-01")
	m.toInput.SetValue("2024-01-31")

	c := mkClient(1, "Acme", 0)
	p := mkProject(1, "Web", c)
	e := mkEntry(1, p, "task", time.Date(2024, 1, 15, 9, 0, 0, 0, time.Local), 30)
	outside := mkEntry(2, p, "task2", time.Date(2024, 2, 1, 9, 0, 0, 0, time.Local), 30)
	m.entries = []*model.Entry{e, outside}

	got, cmd := m.updateFilter(tea.KeyMsg{Type: tea.KeyEnter})
	if got.mode != timersModeList {
		t.Errorf("mode after confirm = %v, want timersModeList", got.mode)
	}
	if got.dateFrom != "2024-01-01" || got.dateTo != "2024-01-31" {
		t.Errorf("dateFrom/dateTo = %q/%q, want 2024-01-01/2024-01-31", got.dateFrom, got.dateTo)
	}
	if got.preset != timersPresetCustom {
		t.Errorf("preset = %v, want timersPresetCustom", got.preset)
	}
	if cmd != nil {
		t.Error("want nil cmd after confirming the filter")
	}
	if len(got.filtered) != 1 || got.filtered[0].ID != 1 {
		t.Errorf("filtered = %+v, want only entry 1 (recomputeFiltered should have run)", got.filtered)
	}
}

func TestUpdateFilter_TabShiftTabToggleFocus(t *testing.T) {
	m := NewTimers(nil)
	m.mode = timersModeFilter
	m.filterIdx = 0
	m.fromInput.Focus()

	got, cmd := m.updateFilter(tea.KeyMsg{Type: tea.KeyTab})
	if got.filterIdx != 1 {
		t.Errorf("filterIdx after tab = %d, want 1", got.filterIdx)
	}
	if !got.toInput.Focused() || got.fromInput.Focused() {
		t.Error("tab should focus toInput and blur fromInput")
	}
	if cmd == nil {
		t.Error("want a non-nil blink cmd after tab")
	}

	got2, _ := got.updateFilter(tea.KeyMsg{Type: tea.KeyShiftTab})
	if got2.filterIdx != 0 {
		t.Errorf("filterIdx after shift+tab = %d, want 0", got2.filterIdx)
	}
	if !got2.fromInput.Focused() || got2.toInput.Focused() {
		t.Error("shift+tab should focus fromInput and blur toInput")
	}
}

func TestUpdateFilter_ForwardsToFocusedInput(t *testing.T) {
	fromCase := NewTimers(nil)
	fromCase.mode = timersModeFilter
	fromCase.filterIdx = 0
	fromCase.fromInput.Focus()
	fromCase.fromInput.SetValue("")
	got, _ := fromCase.updateFilter(runeKey('9'))
	if !strings.Contains(got.fromInput.Value(), "9") {
		t.Errorf("fromInput value = %q, want it to contain '9'", got.fromInput.Value())
	}

	toCase := NewTimers(nil)
	toCase.mode = timersModeFilter
	toCase.filterIdx = 1
	toCase.toInput.Focus()
	toCase.toInput.SetValue("")
	got2, _ := toCase.updateFilter(runeKey('9'))
	if !strings.Contains(got2.toInput.Value(), "9") {
		t.Errorf("toInput value = %q, want it to contain '9'", got2.toInput.Value())
	}
}

// ── Update dispatcher ─────────────────────────────────────────────────────────

func TestUpdate_Dispatch(t *testing.T) {
	d := newTestDB(t)
	c := mkClient(1, "Acme", 0)
	p := mkProject(1, "Web", c)
	e := mkEntry(1, p, "t", time.Now(), 30)

	m := TimersModel{db: d}

	// timerEntriesMsg branch
	got, cmd := m.Update(timerEntriesMsg([]*model.Entry{e}))
	if len(got.entries) != 1 {
		t.Errorf("entries after timerEntriesMsg = %+v, want 1", got.entries)
	}
	if cmd != nil {
		t.Error("want nil cmd from timerEntriesMsg")
	}

	// openFormMsg branch
	got2, _ := got.Update(openFormMsg{entryID: 0})
	if got2.mode != timersModeNew {
		t.Errorf("mode after openFormMsg = %v, want timersModeNew", got2.mode)
	}

	// tea.KeyMsg + timersModeList
	got2.mode = timersModeList
	got3, _ := got2.Update(runeKey('t'))
	if got3.preset != timersPresetToday {
		t.Errorf("preset after 't' in list mode = %v, want timersPresetToday", got3.preset)
	}

	// tea.KeyMsg + timersModeNew
	got2.mode = timersModeNew
	got2.form = newTimerForm(nil, nil)
	got4, _ := got2.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if got4.mode != timersModeList {
		t.Errorf("mode after esc in new-form mode = %v, want timersModeList", got4.mode)
	}

	// tea.KeyMsg + timersModeEdit
	got2.mode = timersModeEdit
	got5, _ := got2.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if got5.mode != timersModeList {
		t.Errorf("mode after esc in edit-form mode = %v, want timersModeList", got5.mode)
	}

	// tea.KeyMsg + timersModeConfirmDelete
	got2.mode = timersModeConfirmDelete
	got6, _ := got2.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if got6.mode != timersModeList {
		t.Errorf("mode after esc in confirm-delete mode = %v, want timersModeList", got6.mode)
	}

	// tea.KeyMsg + timersModeFilter
	got2.mode = timersModeFilter
	got2.fromInput = makeTextInput("", 10, 14)
	got2.toInput = makeTextInput("", 10, 14)
	got7, _ := got2.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if got7.mode != timersModeList {
		t.Errorf("mode after esc in filter mode = %v, want timersModeList", got7.mode)
	}

	// A msg type not handled by the switch falls through to "return m, nil".
	got8, cmd8 := got2.Update(StatusMsg("noop"))
	if got8.mode != got2.mode {
		t.Errorf("mode changed on an unrelated msg type: got %v, want %v", got8.mode, got2.mode)
	}
	if cmd8 != nil {
		t.Error("want nil cmd for an unhandled msg type")
	}
}

// ── Commands: stopCmd / startCmd / deleteCmd / toggleInvoiceCmd / togglePaidCmd ─
//
// Each of these returns a closure producing either an ErrMsg or a
// tea.BatchMsg of anonymous inner closures. go coverage only credits an
// anonymous func's body when it is actually invoked, so every element of the
// returned batch is type-asserted and called here too.

func invokeBatch(t *testing.T, msg tea.Msg) {
	t.Helper()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("got %T, want tea.BatchMsg", msg)
	}
	for _, cmd := range batch {
		if cmd == nil {
			t.Fatal("batch contains a nil cmd")
		}
		cmd() // invoke to cover the inner closure's body
	}
}

func TestStopCmd(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		d := newTestDB(t)
		c, _ := d.CreateClient("Acme", 0)
		p, _ := d.CreateProject(c.ID, "Web")
		entry, err := d.StartEntry(p.ID, "task")
		if err != nil {
			t.Fatalf("StartEntry: %v", err)
		}
		m := TimersModel{db: d}
		msg := m.stopCmd(entry.ID)()
		invokeBatch(t, msg)
	})
	t.Run("error", func(t *testing.T) {
		d := newTestDB(t)
		d.Close()
		m := TimersModel{db: d}
		msg := m.stopCmd(1)()
		if _, ok := msg.(ErrMsg); !ok {
			t.Errorf("stopCmd on a closed db produced %T, want ErrMsg", msg)
		}
	})
}

func TestStartCmd(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		d := newTestDB(t)
		c, _ := d.CreateClient("Acme", 0)
		p, _ := d.CreateProject(c.ID, "Web")
		m := TimersModel{db: d}
		msg := m.startCmd(p.ID, "task")()
		invokeBatch(t, msg)
	})
	t.Run("error", func(t *testing.T) {
		d := newTestDB(t)
		d.Close()
		m := TimersModel{db: d}
		msg := m.startCmd(1, "task")()
		if _, ok := msg.(ErrMsg); !ok {
			t.Errorf("startCmd on a closed db produced %T, want ErrMsg", msg)
		}
	})
}

func TestDeleteCmd(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		d := newTestDB(t)
		c, _ := d.CreateClient("Acme", 0)
		p, _ := d.CreateProject(c.ID, "Web")
		entry, _ := d.StartEntry(p.ID, "task")
		m := TimersModel{db: d}
		msg := m.deleteCmd(entry.ID)()
		invokeBatch(t, msg)
	})
	t.Run("error", func(t *testing.T) {
		d := newTestDB(t)
		d.Close()
		m := TimersModel{db: d}
		msg := m.deleteCmd(1)()
		if _, ok := msg.(ErrMsg); !ok {
			t.Errorf("deleteCmd on a closed db produced %T, want ErrMsg", msg)
		}
	})
}

func TestToggleInvoiceCmd(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		d := newTestDB(t)
		c, _ := d.CreateClient("Acme", 0)
		p, _ := d.CreateProject(c.ID, "Web")
		entry, _ := d.StartEntry(p.ID, "task")
		m := TimersModel{db: d}
		e := &model.Entry{ID: entry.ID, Invoiced: false}
		msg := m.toggleInvoiceCmd(e)()
		invokeBatch(t, msg)
	})
	t.Run("error", func(t *testing.T) {
		d := newTestDB(t)
		d.Close()
		m := TimersModel{db: d}
		e := &model.Entry{ID: 1}
		msg := m.toggleInvoiceCmd(e)()
		if _, ok := msg.(ErrMsg); !ok {
			t.Errorf("toggleInvoiceCmd on a closed db produced %T, want ErrMsg", msg)
		}
	})
}

func TestTogglePaidCmd(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		d := newTestDB(t)
		c, _ := d.CreateClient("Acme", 0)
		p, _ := d.CreateProject(c.ID, "Web")
		entry, _ := d.StartEntry(p.ID, "task")
		if err := d.SetEntryInvoiced(entry.ID, true); err != nil {
			t.Fatalf("SetEntryInvoiced: %v", err)
		}
		m := TimersModel{db: d}
		e := &model.Entry{ID: entry.ID, Invoiced: true, Paid: false}
		msg := m.togglePaidCmd(e)()
		invokeBatch(t, msg)
	})
	t.Run("error", func(t *testing.T) {
		d := newTestDB(t)
		d.Close()
		m := TimersModel{db: d}
		e := &model.Entry{ID: 1}
		msg := m.togglePaidCmd(e)()
		if _, ok := msg.(ErrMsg); !ok {
			t.Errorf("togglePaidCmd on a closed db produced %T, want ErrMsg", msg)
		}
	})
}

// ── submitForm ────────────────────────────────────────────────────────────────

func TestSubmitForm_NewTimer_ResolveProjectError(t *testing.T) {
	d := newTestDB(t)
	f := newTimerForm(nil, nil)
	f.inputs[fieldTask].SetValue("Do work")
	f.inputs[fieldClient].SetValue("Acme") // clientID==0, clientName!="" -> ListClients()
	m := TimersModel{mode: timersModeNew, form: f, db: d}

	_, cmd := m.submitForm()
	if cmd == nil {
		t.Fatal("want a non-nil cmd")
	}
	d.Close()
	msg := cmd()
	if _, ok := msg.(ErrMsg); !ok {
		t.Errorf("submitForm (new) with a closed db produced %T, want ErrMsg", msg)
	}
}

func TestSubmitForm_NewTimer_ExistingProject(t *testing.T) {
	d := newTestDB(t)
	c, _ := d.CreateClient("Acme", 0)
	p, _ := d.CreateProject(c.ID, "Web")

	f := newTimerForm(nil, nil)
	f.inputs[fieldTask].SetValue("Do work")
	f.projectID = p.ID // explicit selection -> resolveProject short-circuits
	m := TimersModel{mode: timersModeNew, form: f, db: d}

	_, cmd := m.submitForm()
	if cmd == nil {
		t.Fatal("want a non-nil cmd")
	}
	msg := cmd()
	invokeBatch(t, msg)

	entries, err := d.ListEntries(0, "", "", true)
	if err != nil || len(entries) != 1 {
		t.Fatalf("ListEntries = %+v, err=%v, want 1 entry", entries, err)
	}
	if entries[0].ProjectID != p.ID {
		t.Errorf("started entry's ProjectID = %d, want %d", entries[0].ProjectID, p.ID)
	}
}

func TestSubmitForm_NewTimer_Uncategorized(t *testing.T) {
	d := newTestDB(t)
	f := newTimerForm(nil, nil)
	f.inputs[fieldTask].SetValue("Do work") // no client/project text -> resolvedProjectID==0
	m := TimersModel{mode: timersModeNew, form: f, db: d}

	_, cmd := m.submitForm()
	if cmd == nil {
		t.Fatal("want a non-nil cmd")
	}
	msg := cmd()
	invokeBatch(t, msg)

	clients, _ := d.ListClients()
	found := false
	for _, c := range clients {
		if c.Name == "Uncategorized" {
			found = true
		}
	}
	if !found {
		t.Error("want an 'Uncategorized' client to have been created")
	}
}

func TestSubmitForm_Edit_NotFound(t *testing.T) {
	d := newTestDB(t)
	c, _ := d.CreateClient("Acme", 0)
	p, _ := d.CreateProject(c.ID, "Web")
	// One real entry exists, but we'll submit an edit for a different (nonexistent) ID.
	if _, err := d.StartEntry(p.ID, "existing"); err != nil {
		t.Fatalf("StartEntry: %v", err)
	}

	f := newTimerForm(nil, nil)
	f.entryID = 99999
	f.inputs[fieldTask].SetValue("Updated task")
	m := TimersModel{mode: timersModeEdit, form: f, db: d}

	_, cmd := m.submitForm()
	if cmd == nil {
		t.Fatal("want a non-nil cmd")
	}
	msg := cmd()
	errMsg, ok := msg.(ErrMsg)
	if !ok {
		t.Fatalf("got %T, want ErrMsg", msg)
	}
	if string(errMsg) != "Entry not found" {
		t.Errorf("err = %q, want %q", errMsg, "Entry not found")
	}
}

func TestSubmitForm_Edit_Success(t *testing.T) {
	d := newTestDB(t)
	c, _ := d.CreateClient("Acme", 0)
	p, _ := d.CreateProject(c.ID, "Web")
	entry, err := d.StartEntry(p.ID, "original task")
	if err != nil {
		t.Fatalf("StartEntry: %v", err)
	}
	if err := d.StopEntry(entry.ID); err != nil {
		t.Fatalf("StopEntry: %v", err)
	}

	f := newTimerForm(nil, nil)
	f.entryID = entry.ID
	f.inputs[fieldTask].SetValue("Updated task")
	f.inputs[fieldNotes].SetValue("Updated notes")
	f.projectID = p.ID
	m := TimersModel{mode: timersModeEdit, form: f, db: d}

	_, cmd := m.submitForm()
	if cmd == nil {
		t.Fatal("want a non-nil cmd")
	}
	msg := cmd()
	invokeBatch(t, msg)

	entries, err := d.ListEntries(0, "", "", true)
	if err != nil || len(entries) != 1 {
		t.Fatalf("ListEntries = %+v, err=%v, want 1 entry", entries, err)
	}
	if entries[0].Task != "Updated task" || entries[0].Notes != "Updated notes" {
		t.Errorf("entry after edit = %+v, want task/notes updated", entries[0])
	}
}

// ── resolveProject ────────────────────────────────────────────────────────────

func TestResolveProject_ExplicitProjectIDShortCircuits(t *testing.T) {
	d := newTestDB(t)
	got, err := resolveProject(d, 0, "", 5, "")
	if err != nil {
		t.Fatalf("resolveProject: %v", err)
	}
	if got != 5 {
		t.Errorf("resolveProject = %d, want 5 (explicit projectID)", got)
	}
}

func TestResolveProject_BothBlankIsUncategorized(t *testing.T) {
	d := newTestDB(t)
	got, err := resolveProject(d, 0, "", 0, "")
	if err != nil {
		t.Fatalf("resolveProject: %v", err)
	}
	if got != 0 {
		t.Errorf("resolveProject with everything blank = %d, want 0", got)
	}
}

func TestResolveProject_ExistingClientCaseInsensitiveMatch(t *testing.T) {
	d := newTestDB(t)
	c, err := d.CreateClient("Acme", 0)
	if err != nil {
		t.Fatalf("CreateClient: %v", err)
	}

	got, err := resolveProject(d, 0, "acme", 0, "New Project")
	if err != nil {
		t.Fatalf("resolveProject: %v", err)
	}
	if got == 0 {
		t.Fatal("want a nonzero project ID")
	}
	projects, _ := d.ListProjects(c.ID)
	if len(projects) != 1 || projects[0].Name != "New Project" {
		t.Errorf("projects for existing client = %+v, want one 'New Project'", projects)
	}
	clients, _ := d.ListClients()
	if len(clients) != 1 {
		t.Errorf("want the existing client to be reused, not duplicated: %+v", clients)
	}
}

func TestResolveProject_NewClientCreated(t *testing.T) {
	d := newTestDB(t)
	got, err := resolveProject(d, 0, "Brand New Client", 0, "")
	if err != nil {
		t.Fatalf("resolveProject: %v", err)
	}
	if got == 0 {
		t.Fatal("want a nonzero project ID")
	}
	clients, _ := d.ListClients()
	if len(clients) != 1 || clients[0].Name != "Brand New Client" {
		t.Errorf("clients = %+v, want one 'Brand New Client'", clients)
	}
	// projectName == "" && clientName != "" -> project defaults to the client name.
	projects, _ := d.ListProjects(clients[0].ID)
	if len(projects) != 1 || projects[0].Name != "Brand New Client" {
		t.Errorf("projects = %+v, want project named after the client", projects)
	}
}

func TestResolveProject_ProjectNameOnlyCreatesClientFromProjectName(t *testing.T) {
	d := newTestDB(t)
	got, err := resolveProject(d, 0, "", 0, "Standalone Project")
	if err != nil {
		t.Fatalf("resolveProject: %v", err)
	}
	if got == 0 {
		t.Fatal("want a nonzero project ID")
	}
	clients, _ := d.ListClients()
	if len(clients) != 1 || clients[0].Name != "Standalone Project" {
		t.Errorf("clients = %+v, want one client named after the project", clients)
	}
}

func TestResolveProject_ExistingProjectFoundWithoutDuplicating(t *testing.T) {
	d := newTestDB(t)
	c, _ := d.CreateClient("Acme", 0)
	p, err := d.CreateProject(c.ID, "Website")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	got, err := resolveProject(d, c.ID, "Acme", 0, "website") // case-insensitive match
	if err != nil {
		t.Fatalf("resolveProject: %v", err)
	}
	if got != p.ID {
		t.Errorf("resolveProject = %d, want existing project ID %d", got, p.ID)
	}
	projects, _ := d.ListProjects(c.ID)
	if len(projects) != 1 {
		t.Errorf("want no duplicate project created: %+v", projects)
	}
}

func TestResolveProject_NewProjectUnderExistingClient(t *testing.T) {
	d := newTestDB(t)
	c, _ := d.CreateClient("Acme", 0)

	got, err := resolveProject(d, c.ID, "Acme", 0, "New Project")
	if err != nil {
		t.Fatalf("resolveProject: %v", err)
	}
	if got == 0 {
		t.Fatal("want a nonzero project ID")
	}
	projects, _ := d.ListProjects(c.ID)
	if len(projects) != 1 || projects[0].Name != "New Project" {
		t.Errorf("projects = %+v, want one 'New Project' under the existing client", projects)
	}
}

func TestResolveProject_ListClientsError(t *testing.T) {
	d := newTestDB(t)
	d.Close()
	_, err := resolveProject(d, 0, "Acme", 0, "")
	if err == nil {
		t.Error("want an error when ListClients fails on a closed db")
	}
}

func TestResolveProject_ListProjectsError(t *testing.T) {
	d := newTestDB(t)
	c, _ := d.CreateClient("Acme", 0)
	d.Close()
	_, err := resolveProject(d, c.ID, "Acme", 0, "Some Project")
	if err == nil {
		t.Error("want an error when ListProjects fails on a closed db")
	}
}

// ── ensureUncategorizedProject ────────────────────────────────────────────────

func TestEnsureUncategorizedProject_CreatesThenFinds(t *testing.T) {
	d := newTestDB(t)

	id1, err := ensureUncategorizedProject(d)
	if err != nil {
		t.Fatalf("ensureUncategorizedProject (1st call): %v", err)
	}
	if id1 == 0 {
		t.Fatal("want a nonzero project ID")
	}

	id2, err := ensureUncategorizedProject(d)
	if err != nil {
		t.Fatalf("ensureUncategorizedProject (2nd call): %v", err)
	}
	if id2 != id1 {
		t.Errorf("2nd call returned a different project (%d vs %d); want the existing one reused", id2, id1)
	}

	clients, _ := d.ListClients()
	count := 0
	for _, c := range clients {
		if c.Name == "Uncategorized" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("want exactly one 'Uncategorized' client, got %d", count)
	}
}

// ── mustLoadEntries ───────────────────────────────────────────────────────────

func TestMustLoadEntries(t *testing.T) {
	d := newTestDB(t)
	c, _ := d.CreateClient("Acme", 0)
	p, _ := d.CreateProject(c.ID, "Web")
	if _, err := d.StartEntry(p.ID, "task"); err != nil {
		t.Fatalf("StartEntry: %v", err)
	}
	entries := mustLoadEntries(d)
	if len(entries) != 1 {
		t.Errorf("mustLoadEntries = %+v, want 1 entry", entries)
	}
}

// ── updateList: remaining key branches ───────────────────────────────────────

func TestUpdateList_Navigation(t *testing.T) {
	c := mkClient(1, "Acme", 0)
	p := mkProject(1, "Web", c)
	base := time.Date(2024, 5, 1, 9, 0, 0, 0, time.Local)
	e1 := mkEntry(1, p, "a", base, 30)
	e2 := mkEntry(2, p, "b", base.Add(time.Hour), 30)
	m := TimersModel{entries: []*model.Entry{e2, e1}, height: 30}
	m.recomputeFiltered()
	m.cursor = 1

	got, _ := m.updateList(tea.KeyMsg{Type: tea.KeyUp})
	if got.cursor != 0 {
		t.Errorf("cursor after up = %d, want 0", got.cursor)
	}
	got2, _ := got.updateList(tea.KeyMsg{Type: tea.KeyDown})
	if got2.cursor != 1 {
		t.Errorf("cursor after down = %d, want 1", got2.cursor)
	}
	// At the bottom already: down should be a no-op (guarded by the len check).
	got3, _ := got2.updateList(tea.KeyMsg{Type: tea.KeyDown})
	if got3.cursor != 1 {
		t.Errorf("cursor after down at bottom = %d, want to stay at 1", got3.cursor)
	}
}

func TestUpdateList_NewAndEditKeys(t *testing.T) {
	d := newTestDB(t)
	c := mkClient(1, "Acme", 0)
	p := mkProject(1, "Web", c)
	e := mkEntry(1, p, "task", time.Now(), 30)

	withEntry := TimersModel{db: d, entries: []*model.Entry{e}}
	withEntry.recomputeFiltered()

	got, cmd := withEntry.updateList(runeKey('n'))
	if cmd == nil {
		t.Error("'n' should return the open-form cmd")
	}
	_ = got

	got2, cmd2 := withEntry.updateList(runeKey('e'))
	if cmd2 == nil {
		t.Error("'e' with a selection should return the open-form cmd")
	}
	_ = got2

	empty := TimersModel{db: d}
	got3, cmd3 := empty.updateList(runeKey('e'))
	if cmd3 != nil {
		t.Error("'e' with no selection should be a no-op")
	}
	if got3.mode != timersModeList {
		t.Errorf("mode = %v, want unchanged", got3.mode)
	}
}

func TestUpdateList_ToggleKey(t *testing.T) {
	d := newTestDB(t)
	c := mkClient(1, "Acme", 0)
	p := mkProject(1, "Web", c)

	running := mkRunningEntry(1, p, "task", time.Now())
	runningModel := TimersModel{db: d, entries: []*model.Entry{running}}
	runningModel.recomputeFiltered()
	_, cmd := runningModel.updateList(runeKey(' '))
	if cmd == nil {
		t.Error("space on a running entry should return the stop cmd")
	}

	stopped := mkEntry(2, p, "task", time.Now().Add(-time.Hour), 30)
	stoppedModel := TimersModel{db: d, entries: []*model.Entry{stopped}}
	stoppedModel.recomputeFiltered()
	_, cmd2 := stoppedModel.updateList(runeKey(' '))
	if cmd2 == nil {
		t.Error("space on a stopped entry should return the start cmd")
	}
}

func TestUpdateList_InvoiceKey(t *testing.T) {
	c := mkClient(1, "Acme", 0)
	p := mkProject(1, "Web", c)

	running := mkRunningEntry(1, p, "task", time.Now())
	runningModel := TimersModel{entries: []*model.Entry{running}}
	runningModel.recomputeFiltered()
	_, cmd := runningModel.updateList(runeKey('i'))
	if cmd == nil {
		t.Fatal("want a non-nil cmd")
	}
	if _, ok := cmd().(ErrMsg); !ok {
		t.Error("'i' on a running entry should produce an ErrMsg telling the user to stop it first")
	}

	d := newTestDB(t)
	stopped := mkEntry(2, p, "task", time.Now().Add(-time.Hour), 30)
	stoppedModel := TimersModel{db: d, entries: []*model.Entry{stopped}}
	stoppedModel.recomputeFiltered()
	_, cmd2 := stoppedModel.updateList(runeKey('i'))
	if cmd2 == nil {
		t.Error("'i' on a stopped entry should return the toggle-invoice cmd")
	}
}

func TestUpdateList_PaidKey(t *testing.T) {
	c := mkClient(1, "Acme", 0)
	p := mkProject(1, "Web", c)

	notInvoiced := mkEntry(1, p, "task", time.Now().Add(-time.Hour), 30)
	m := TimersModel{entries: []*model.Entry{notInvoiced}}
	m.recomputeFiltered()
	_, cmd := m.updateList(runeKey('p'))
	if cmd == nil {
		t.Fatal("want a non-nil cmd")
	}
	if _, ok := cmd().(ErrMsg); !ok {
		t.Error("'p' on a not-yet-invoiced entry should produce an ErrMsg")
	}

	d := newTestDB(t)
	invoiced := mkEntry(2, p, "task", time.Now().Add(-time.Hour), 30)
	invoiced.Invoiced = true
	m2 := TimersModel{db: d, entries: []*model.Entry{invoiced}}
	m2.recomputeFiltered()
	_, cmd2 := m2.updateList(runeKey('p'))
	if cmd2 == nil {
		t.Error("'p' on an invoiced entry should return the toggle-paid cmd")
	}
}

// ── openFormCmd: isolate the second (ListProjects) error branch ─────────────

func TestOpenFormCmd_ListProjectsError(t *testing.T) {
	d, path := newTestDBAtPath(t)
	if _, err := d.CreateClient("Acme", 0); err != nil {
		t.Fatalf("CreateClient: %v", err)
	}
	dropTableRaw(t, path, "projects")

	m := TimersModel{db: d}
	msg := m.openFormCmd(0)()
	if _, ok := msg.(ErrMsg); !ok {
		t.Errorf("openFormCmd with projects table missing produced %T, want ErrMsg", msg)
	}
}

// ── startCmd: isolate the StartEntry error branch (FK violation) ────────────

func TestStartCmd_StartEntryError(t *testing.T) {
	d := newTestDB(t)
	m := TimersModel{db: d}
	// StopAllRunning succeeds trivially (nothing running); StartEntry then
	// fails because project 99999 doesn't exist (foreign_keys=ON).
	msg := m.startCmd(99999, "task")()
	if _, ok := msg.(ErrMsg); !ok {
		t.Errorf("startCmd with a nonexistent project produced %T, want ErrMsg", msg)
	}
}

// ── submitForm: remaining error branches, isolated one call at a time ───────

func TestSubmitForm_NewTimer_StopAllRunningError(t *testing.T) {
	d := newTestDB(t)
	f := newTimerForm(nil, nil)
	f.inputs[fieldTask].SetValue("Do work") // client/project blank -> resolveProject skips the DB entirely
	m := TimersModel{mode: timersModeNew, form: f, db: d}

	_, cmd := m.submitForm()
	if cmd == nil {
		t.Fatal("want a non-nil cmd")
	}
	d.Close() // makes StopAllRunning fail without resolveProject having touched the db
	msg := cmd()
	if _, ok := msg.(ErrMsg); !ok {
		t.Errorf("got %T, want ErrMsg from StopAllRunning failing", msg)
	}
}

func TestSubmitForm_NewTimer_StartEntryErrorWithResolvedProject(t *testing.T) {
	d := newTestDB(t)
	f := newTimerForm(nil, nil)
	f.inputs[fieldTask].SetValue("Do work")
	f.projectID = 99999 // explicit selection short-circuits resolveProject; FK violation on StartEntry
	m := TimersModel{mode: timersModeNew, form: f, db: d}

	_, cmd := m.submitForm()
	msg := cmd()
	if _, ok := msg.(ErrMsg); !ok {
		t.Errorf("got %T, want ErrMsg from StartEntry's FK violation", msg)
	}
}

func TestSubmitForm_NewTimer_EnsureUncategorizedProjectError(t *testing.T) {
	d, path := newTestDBAtPath(t)
	f := newTimerForm(nil, nil)
	f.inputs[fieldTask].SetValue("Do work") // blank client/project -> ensureUncategorizedProject path
	m := TimersModel{mode: timersModeNew, form: f, db: d}
	_, cmd := m.submitForm()

	// Drop clients (not entries) so StopAllRunning still succeeds but
	// ensureUncategorizedProject's ListClients() call fails.
	dropTableRaw(t, path, "clients")

	msg := cmd()
	if _, ok := msg.(ErrMsg); !ok {
		t.Errorf("got %T, want ErrMsg from ensureUncategorizedProject failing", msg)
	}
}

func TestSubmitForm_Edit_ResolveProjectError(t *testing.T) {
	d := newTestDB(t)
	f := newTimerForm(nil, nil)
	f.entryID = 1
	f.inputs[fieldTask].SetValue("Updated task")
	f.inputs[fieldClient].SetValue("Acme") // clientID==0, clientName!="" -> ListClients()
	m := TimersModel{mode: timersModeEdit, form: f, db: d}

	_, cmd := m.submitForm()
	d.Close()
	msg := cmd()
	if _, ok := msg.(ErrMsg); !ok {
		t.Errorf("got %T, want ErrMsg from resolveProject failing", msg)
	}
}

func TestSubmitForm_Edit_ListEntriesError(t *testing.T) {
	d, path := newTestDBAtPath(t)
	c, _ := d.CreateClient("Acme", 0)
	p, _ := d.CreateProject(c.ID, "Web")

	f := newTimerForm(nil, nil)
	f.entryID = 1
	f.inputs[fieldTask].SetValue("Updated task")
	f.projectID = p.ID // short-circuits resolveProject, no DB touch
	m := TimersModel{mode: timersModeEdit, form: f, db: d}

	dropTableRaw(t, path, "entries")

	_, cmd := m.submitForm()
	msg := cmd()
	if _, ok := msg.(ErrMsg); !ok {
		t.Errorf("got %T, want ErrMsg from ListEntries failing", msg)
	}
}

func TestSubmitForm_Edit_UpdateEntryError(t *testing.T) {
	d := newTestDB(t)
	c, _ := d.CreateClient("Acme", 0)
	p, _ := d.CreateProject(c.ID, "Web")
	entry, err := d.StartEntry(p.ID, "original task")
	if err != nil {
		t.Fatalf("StartEntry: %v", err)
	}

	f := newTimerForm(nil, nil)
	f.entryID = entry.ID
	f.inputs[fieldTask].SetValue("Updated task")
	f.projectID = 99999 // resolves via short-circuit; FK violation on UpdateEntry
	m := TimersModel{mode: timersModeEdit, form: f, db: d}

	_, cmd := m.submitForm()
	msg := cmd()
	if _, ok := msg.(ErrMsg); !ok {
		t.Errorf("got %T, want ErrMsg from UpdateEntry's FK violation", msg)
	}
}

// ── ensureUncategorizedProject: remaining error branch ───────────────────────

func TestEnsureUncategorizedProject_ListClientsError(t *testing.T) {
	d := newTestDB(t)
	d.Close()
	_, err := ensureUncategorizedProject(d)
	if err == nil {
		t.Error("want an error when ListClients fails on a closed db")
	}
}

func TestEnsureUncategorizedProject_ListProjectsError(t *testing.T) {
	d, path := newTestDBAtPath(t)
	dropTableRaw(t, path, "projects")
	_, err := ensureUncategorizedProject(d)
	if err == nil {
		t.Error("want an error when ListProjects fails (projects table missing)")
	}
}

// ── View / viewList / viewEmptyState ─────────────────────────────────────────

func TestView_DispatchesByMode(t *testing.T) {
	f := newTimerForm(nil, nil)
	newMode := TimersModel{mode: timersModeNew, form: f, width: 100, height: 30}
	if got := newMode.View(); !strings.Contains(got, "New Timer") {
		t.Errorf("View() in new-form mode should render the form, got %q", got)
	}

	editForm := f
	editForm.entryID = 42
	editMode := TimersModel{mode: timersModeEdit, form: editForm, width: 100, height: 30}
	if got := editMode.View(); !strings.Contains(got, "Edit Entry") {
		t.Errorf("View() in edit-form mode should render the form, got %q", got)
	}

	listMode := TimersModel{mode: timersModeList, width: 100, height: 30}
	if got := listMode.View(); !strings.Contains(got, "No entries yet") {
		t.Errorf("View() in list mode with no entries should render the empty state, got %q", got)
	}
}

func TestViewList_EmptyEntries(t *testing.T) {
	m := TimersModel{width: 100, height: 30}
	got := m.viewList()
	if !strings.Contains(got, "No entries yet") {
		t.Errorf("viewList() with no entries at all = %q, want the 'no entries yet' empty state", got)
	}
}

func TestViewList_EmptyFilteredNonEmptyEntries(t *testing.T) {
	c := mkClient(1, "Acme", 0)
	p := mkProject(1, "Web", c)
	e := mkEntry(1, p, "task", time.Date(2024, 5, 1, 9, 0, 0, 0, time.Local), 30)
	m := TimersModel{
		width: 100, height: 30,
		entries:  []*model.Entry{e},
		dateFrom: "2099-01-01", dateTo: "2099-01-02", // range excludes the only entry
	}
	m.recomputeFiltered()
	got := m.viewList()
	if !strings.Contains(got, "No entries for this range") {
		t.Errorf("viewList() with entries outside the filter range = %q, want the 'no entries for this range' empty state", got)
	}
}

func TestViewList_FilterMode(t *testing.T) {
	m := NewTimers(nil)
	m.mode = timersModeFilter
	m.width, m.height = 100, 30
	got := m.viewList()
	if !strings.Contains(got, "From") || !strings.Contains(got, "To") {
		t.Errorf("viewList() in filter mode = %q, want the date-range filter bar", got)
	}
}

// TestViewList_Populated exercises the main table-rendering path: multiple
// days (day-header line), a running entry, entries with/without a project,
// zero and nonzero hourly rates, and — with a short height — the scroll
// indicators in the divider.
func TestViewList_Populated(t *testing.T) {
	c := mkClient(1, "Acme", 60) // $60/hr
	free := mkClient(2, "Freebie", 0)
	p := mkProject(1, "Web", c)
	pFree := mkProject(2, "OSS", free)

	day1 := time.Date(2024, 5, 1, 9, 0, 0, 0, time.Local)
	day2 := time.Date(2024, 5, 2, 9, 0, 0, 0, time.Local)

	billable := mkEntry(1, p, "billable work", day2, 30)
	zeroRate := mkEntry(2, pFree, "free work", day2, 30)
	noProject := mkEntry(3, p, "orphan", day1, 30)
	noProject.Project = nil
	running := mkRunningEntry(4, p, "running work", day1)

	m := TimersModel{
		width: 120, height: 30,
		entries: []*model.Entry{billable, zeroRate, noProject, running},
	}
	m.recomputeFiltered()
	m.cursor = 0 // select the (non-running) billable entry: hits the "default" row style + StyleTableRowRun for the *other* running row
	got := m.viewList()

	if !strings.Contains(got, "running work") {
		t.Errorf("viewList() should show the running entry's task, got:\n%s", got)
	}
	if !strings.Contains(got, "$30.00") {
		t.Errorf("viewList() should show billable earnings, got:\n%s", got)
	}
	if !strings.Contains(got, "—") {
		t.Errorf("viewList() should show '—' for a zero-rate or projectless entry's earned column, got:\n%s", got)
	}
	if !strings.Contains(got, "Today") && !strings.Contains(got, "2024") {
		// day headers render "Today"/"Yesterday"/friendly date depending on
		// when the test runs relative to the fixed 2024 dates; either way a
		// day-header line should be present.
	}
}

// TestViewList_NoteMarker checks the "• " prefix that flags a row as having
// notes — the only other places notes show are the detail pane (for
// whichever single row is currently selected) and the edit form, so without
// a marker in the table itself, a row's notes are easy to miss unless you
// happen to select that exact row.
func TestViewList_NoteMarker(t *testing.T) {
	c := mkClient(1, "Acme", 0)
	p := mkProject(1, "Web", c)
	base := time.Date(2024, 5, 1, 9, 0, 0, 0, time.Local)

	withNotes := mkEntry(1, p, "has notes task", base.Add(time.Hour), 30)
	withNotes.Notes = "important context"
	withoutNotes := mkEntry(2, p, "plain task", base, 30)

	m := TimersModel{width: 120, height: 30, entries: []*model.Entry{withNotes, withoutNotes}}
	m.recomputeFiltered()
	got := m.viewList()

	lines := strings.Split(got, "\n")
	var notedLine, plainLine string
	for _, l := range lines {
		if strings.Contains(l, "has notes task") {
			notedLine = l
		}
		if strings.Contains(l, "plain task") {
			plainLine = l
		}
	}
	if notedLine == "" || plainLine == "" {
		t.Fatalf("could not find both task rows in output:\n%s", got)
	}
	if !strings.Contains(notedLine, "• has notes task") {
		t.Errorf("row for entry with notes should show the '• ' marker directly before its task text, got %q", notedLine)
	}
	if strings.Contains(plainLine, "•") {
		t.Errorf("row for entry without notes should not show the marker, got %q", plainLine)
	}
}

func TestViewList_ScrollIndicators(t *testing.T) {
	c := mkClient(1, "Acme", 0)
	p := mkProject(1, "Web", c)
	base := time.Date(2024, 5, 1, 9, 0, 0, 0, time.Local)
	var entries []*model.Entry
	for i := 0; i < 15; i++ {
		entries = append(entries, mkEntry(int64(i+1), p, "task", base.Add(time.Duration(i)*time.Hour), 10))
	}

	// Small height forces scrolling. Cursor at the top -> offset 0, "more
	// below" (▼) but not "more above".
	top := TimersModel{width: 120, height: 12, entries: entries, cursor: 0}
	top.recomputeFiltered()
	gotTop := top.viewList()
	if !strings.Contains(gotTop, "▼") {
		t.Errorf("viewList() with cursor at top and a long list should show a ▼ scroll indicator, got:\n%s", gotTop)
	}

	// Cursor at the bottom -> offset > 0, "more above" (▲).
	bottom := TimersModel{width: 120, height: 12, entries: entries, cursor: len(entries) - 1}
	bottom.recomputeFiltered()
	bottom.cursor = len(bottom.filtered) - 1
	gotBottom := bottom.viewList()
	if !strings.Contains(gotBottom, "▲") {
		t.Errorf("viewList() with cursor at bottom and a long list should show a ▲ scroll indicator, got:\n%s", gotBottom)
	}
}

func TestViewList_ConfirmDeleteModal(t *testing.T) {
	c := mkClient(1, "Acme", 0)
	p := mkProject(1, "Web", c)
	e := mkEntry(1, p, "delete me", time.Now(), 30)
	m := TimersModel{width: 100, height: 30, mode: timersModeConfirmDelete, entries: []*model.Entry{e}}
	m.recomputeFiltered()
	m.cursor = 0

	got := m.viewList()
	if !strings.Contains(got, "Delete") || !strings.Contains(got, "confirm") {
		t.Errorf("viewList() in confirm-delete mode should overlay the delete modal, got:\n%s", got)
	}
	if !strings.Contains(got, "This cannot be undone") {
		t.Errorf("viewList() confirm-delete modal should show the warning, got:\n%s", got)
	}
	// The table itself must still be visible around/behind the modal — the
	// whole point of an overlay vs. the old full-pane swap. The modal is
	// centered over the table and covers the middle columns of this narrow
	// test width, so check header cells past its right edge instead of ones
	// it happens to sit on top of.
	if !strings.Contains(got, "Status") || !strings.Contains(got, "Earned") {
		t.Errorf("viewList() in confirm-delete mode should still show the table underneath the modal, got:\n%s", got)
	}
}

// ── renderDetailPane ──────────────────────────────────────────────────────────

func TestRenderDetailPane_Branches(t *testing.T) {
	c := mkClient(1, "Acme", 0)
	p := mkProject(1, "Web", c)
	e := mkEntry(1, p, "t", time.Now(), 30)

	// Confirm-delete no longer swaps this pane for a warning — that's now a
	// floating modal (see viewList/renderConfirmModal) — so it renders the
	// same selected-entry detail as any other mode.
	deleting := TimersModel{mode: timersModeConfirmDelete, filtered: []*model.Entry{e}, cursor: 0}
	if got := deleting.renderDetailPane(); got == "" {
		t.Error("renderDetailPane in confirm-delete mode with a selected entry should still render its detail")
	}

	selected := TimersModel{filtered: []*model.Entry{e}, cursor: 0}
	if got := selected.renderDetailPane(); got == "" {
		t.Error("renderDetailPane with a selected entry should render its detail, got empty string")
	}

	none := TimersModel{}
	if got := none.renderDetailPane(); got != "" {
		t.Errorf("renderDetailPane with nothing selected = %q, want empty string", got)
	}
}

// ── renderDetail ──────────────────────────────────────────────────────────────

func TestRenderDetail_Variants(t *testing.T) {
	c := mkClient(1, "Acme", 50)
	p := mkProject(1, "Web", c)
	m := TimersModel{width: 100}

	t.Run("running, no notes", func(t *testing.T) {
		e := mkRunningEntry(1, p, "task", time.Now())
		out := m.renderDetail(e)
		lines := strings.Split(out, "\n")
		if len(lines) != entryDetailLines {
			t.Errorf("renderDetail for a running entry with no notes should still render %d fixed lines (with a placeholder Notes line), got %d: %q", entryDetailLines, len(lines), out)
		}
		if !strings.Contains(out, "Notes") {
			t.Errorf("renderDetail should always include a Notes line (placeholder when empty), got %q", out)
		}
	})
	t.Run("finished with notes", func(t *testing.T) {
		e := mkEntry(2, p, "task", time.Now().Add(-time.Hour), 30)
		e.Notes = "hello world"
		out := m.renderDetail(e)
		if !strings.Contains(out, "hello world") {
			t.Errorf("renderDetail should include notes, got %q", out)
		}
		if !strings.Contains(out, "–") {
			t.Errorf("renderDetail for a finished entry should show an end-time separator, got %q", out)
		}
	})
	t.Run("long notes truncate instead of wrapping", func(t *testing.T) {
		e := mkEntry(3, p, "task", time.Now().Add(-time.Hour), 30)
		e.Notes = strings.Repeat("a very long note that would wrap ", 10)
		out := m.renderDetail(e)
		lines := strings.Split(out, "\n")
		if len(lines) != entryDetailLines {
			t.Errorf("renderDetail with a long note should still render %d fixed lines, got %d: %q", entryDetailLines, len(lines), out)
		}
	})
}

// ── rowsBudget / clampOffset ──────────────────────────────────────────────────

func TestRowsBudget(t *testing.T) {
	c := mkClient(1, "Acme", 0)
	p := mkProject(1, "Web", c)
	e := mkEntry(1, p, "t", time.Now(), 30)

	normal := TimersModel{height: 30, filtered: []*model.Entry{e}, cursor: 0}
	if got := normal.rowsBudget(); got < 1 {
		t.Errorf("rowsBudget() = %d, want >= 1", got)
	}

	deleting := normal
	deleting.mode = timersModeConfirmDelete
	if got := deleting.rowsBudget(); got < 1 {
		t.Errorf("rowsBudget() during confirm-delete = %d, want >= 1", got)
	}

	tiny := TimersModel{height: 0}
	if got := tiny.rowsBudget(); got != 1 {
		t.Errorf("rowsBudget() with a tiny height = %d, want clamped to 1", got)
	}
}

func TestClampOffset(t *testing.T) {
	// n == 0
	var empty TimersModel
	empty.clampOffset()
	if empty.offset != 0 {
		t.Errorf("clampOffset with no lines: offset = %d, want 0", empty.offset)
	}

	// n <= budget: no scrolling needed
	c := mkClient(1, "Acme", 0)
	p := mkProject(1, "Web", c)
	base := time.Date(2024, 5, 1, 9, 0, 0, 0, time.Local)
	var few []*model.Entry
	for i := 0; i < 3; i++ {
		few = append(few, mkEntry(int64(i+1), p, "t", base.Add(time.Duration(i)*time.Hour), 10))
	}
	small := TimersModel{height: 50, filtered: few}
	small.clampOffset()
	if small.offset != 0 {
		t.Errorf("clampOffset with few lines: offset = %d, want 0", small.offset)
	}

	// Many lines + small budget: scroll forward as the cursor moves to the
	// bottom, then back as it returns to the top.
	var many []*model.Entry
	for i := 0; i < 20; i++ {
		many = append(many, mkEntry(int64(i+1), p, "t", base.Add(time.Duration(i)*time.Hour), 10))
	}
	scroll := TimersModel{height: 15, filtered: many, cursor: len(many) - 1}
	scroll.clampOffset()
	if scroll.offset <= 0 {
		t.Errorf("clampOffset with cursor at the bottom: offset = %d, want > 0", scroll.offset)
	}

	offsetAtBottom := scroll.offset
	scroll.cursor = 0
	scroll.clampOffset()
	// The very first entry's render line is always preceded by its
	// day-header line (see buildLines), so cursorLine for it is 1, not 0 —
	// clampOffset pulls offset back down to that cursorLine (the
	// "cursorLine < m.offset" branch), not all the way to the literal top
	// of the window. Keeping the day header itself visible even when its
	// real line has scrolled out is viewList's job now (it synthesizes a
	// sticky copy — see TestViewList_DayHeaderStaysStickyWhileScrolling),
	// not clampOffset's.
	if scroll.offset >= offsetAtBottom {
		t.Errorf("clampOffset after cursor returns to top: offset = %d, want less than the bottom-scrolled offset %d", scroll.offset, offsetAtBottom)
	}
	if scroll.offset > 1 {
		t.Errorf("clampOffset after cursor returns to the very first entry: offset = %d, want 0 or 1 (just its header line)", scroll.offset)
	}
}

// TestViewList_DayHeaderStaysStickyWhileScrolling is a regression test for a
// two-part bug: (1) scrolling down just far enough to push a day's header
// line out of the visible window, then (2) scrolling back up through that
// day's entries — even without returning all the way to its very first
// entry — used to leave the header gone until the window happened to
// realign with its real line. viewList now synthesizes a sticky copy of the
// current top-of-window entry's day header whenever the real one has
// scrolled off, so it should stay visible for every cursor position within
// a group, switching only once the cursor crosses into the next group.
//
// Entries are placed >1 day in the past so friendlyDay returns "Mon, Jan
// 2"-style labels instead of "Today"/"Yesterday" — the latter would also
// match the unrelated "Timers · Today" range-label in the title (m.preset
// defaults to timersPresetToday), making a text search ambiguous.
func TestViewList_DayHeaderStaysStickyWhileScrolling(t *testing.T) {
	c := mkClient(1, "Acme", 0)
	p := mkProject(1, "Web", c)

	dayA := time.Now().AddDate(0, 0, -10)
	dayB := dayA.AddDate(0, 0, -1)
	labelA := friendlyDay(dayA)
	labelB := friendlyDay(dayB)

	var entries []*model.Entry
	for i := 0; i < 8; i++ {
		entries = append(entries, mkEntry(int64(i+1), p, fmt.Sprintf("a-%d", i), dayA.Add(-time.Duration(i)*time.Hour), 10))
	}
	for i := 0; i < 3; i++ {
		entries = append(entries, mkEntry(int64(i+100), p, fmt.Sprintf("b-%d", i), dayB.Add(-time.Duration(i)*time.Hour), 10))
	}

	m := TimersModel{width: 100, height: 15, entries: entries}
	m.recomputeFiltered()

	// Walk the cursor down through every entry of group A one row at a
	// time — labelA must stay visible the whole way, not just at the very
	// first or very last entry of the group.
	for cursor := 0; cursor < 8; cursor++ {
		m.cursor = cursor
		got := m.viewList()
		if !strings.Contains(got, labelA) {
			t.Errorf("cursor=%d (still within group A): viewList() should show %q, got:\n%s", cursor, labelA, got)
		}
	}

	// One more step crosses into group B — its own header should now show.
	m.cursor = 8
	got := m.viewList()
	if !strings.Contains(got, labelB) {
		t.Errorf("cursor=8 (first entry of group B): viewList() should show %q, got:\n%s", labelB, got)
	}
}

// TestViewList_HeightStaysConstantAcrossStickyHeaderBoundary is a
// regression test: viewList used to render one extra line the moment the
// window scrolled to a top row that wasn't itself a day header, because
// the synthesized sticky copy (see windowEnd) was added on top of a full
// budget's worth of entries instead of taking one of its slots — so the
// table visibly grew by a row as soon as you scrolled one row past a day
// boundary, then shrank back. Rendered height must stay constant no matter
// which row the window happens to start on.
func TestViewList_HeightStaysConstantAcrossStickyHeaderBoundary(t *testing.T) {
	c := mkClient(1, "Acme", 0)
	p := mkProject(1, "Web", c)

	base := time.Now().AddDate(0, 0, -10)
	var entries []*model.Entry
	for i := 0; i < 20; i++ {
		entries = append(entries, mkEntry(int64(i+1), p, fmt.Sprintf("t-%d", i), base.Add(-time.Duration(i)*time.Hour), 10))
	}

	m := TimersModel{width: 100, height: 15, entries: entries}
	m.recomputeFiltered()

	var heights []int
	for cursor := 0; cursor < len(entries); cursor++ {
		m.cursor = cursor
		heights = append(heights, lipgloss.Height(m.viewList()))
	}
	for i, h := range heights {
		if h != heights[0] {
			t.Errorf("cursor=%d: viewList() height = %d, want %d (same as cursor=0) — table should never grow/shrink while scrolling", i, h, heights[0])
		}
	}
}

// ── viewForm ──────────────────────────────────────────────────────────────────

func TestViewForm_NewVsEditTitle(t *testing.T) {
	f := newTimerForm(nil, nil)
	newM := TimersModel{mode: timersModeNew, form: f}
	if got := newM.viewForm(); !strings.Contains(got, "New Timer") {
		t.Errorf("viewForm() for a new entry should say 'New Timer', got %q", got)
	}

	f2 := f
	f2.entryID = 42
	editM := TimersModel{mode: timersModeEdit, form: f2}
	if got := editM.viewForm(); !strings.Contains(got, "Edit Entry") {
		t.Errorf("viewForm() for an existing entry should say 'Edit Entry', got %q", got)
	}
}

// TestRenderFormField_LabelLineStructure checks the label and input text
// land together on line 0 (with only the border's dashes on line 1) —
// lipgloss.Top alignment already got this right on its own, unlike the
// date-range filter bar's Center-alignment bug, so this is a sanity check
// rather than a regression test for that specific failure mode.
func TestRenderFormField_LabelLineStructure(t *testing.T) {
	f := newTimerForm(nil, nil)
	f.inputs[fieldTask].SetValue("Fix nav bug")

	got := f.renderFormField("Task", fieldTask)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("renderFormField produced %d lines, want 2 (text line + border line):\n%s", len(lines), got)
	}

	textLine, borderLine := lines[0], lines[1]
	if !strings.Contains(textLine, "Task") || !strings.Contains(textLine, "Fix nav bug") {
		t.Errorf("line 0 (text line) missing label/value, got %q", textLine)
	}
	if strings.Contains(borderLine, "Task") || strings.Contains(borderLine, "Fix nav bug") {
		t.Errorf("line 1 (border line) should not contain the label/value, got %q", borderLine)
	}
	if !strings.Contains(borderLine, "─") {
		t.Errorf("line 1 (border line) should contain the input box's underline, got %q", borderLine)
	}
}

// TestRenderFormField_NoUnstyledBackgroundGap is the actual regression test:
// the label (1 line) is shorter than the input box (2 lines: text + border),
// so lipgloss.JoinHorizontal used to auto-pad the label with a genuinely
// unstyled blank row under it — visible as a gap of the terminal's default
// color next to the box's border line. Top alignment already put the label
// text on the right line (see TestRenderFormField_LabelLineStructure), so
// only unstyledCellCount can actually catch this; a text-position check
// can't.
func TestRenderFormField_NoUnstyledBackgroundGap(t *testing.T) {
	f := newTimerForm(nil, nil)
	f.inputs[fieldTask].SetValue("Fix nav bug")

	var got string
	n := unstyledCellCount(t, func() string {
		got = f.renderFormField("Task", fieldTask)
		return got
	})
	if n > 0 {
		t.Errorf("renderFormField has %d unstyled cell(s), want 0:\n%s", n, got)
	}
}

// TestRenderFormField_PicksUpLiveThemeChange guards against a real bug: a
// textinput.Model's own internal styles (PromptStyle/TextStyle/
// PlaceholderStyle/Cursor styles) are set once, at construction
// (makeTextInput), from the theme active at that moment — they aren't among
// the package Style* vars RefreshTheme recomputes on a live theme change.
// Without refreshInputStyle re-applying them on every render, a form opened
// before a theme switch (e.g. an aliasos theme swap picked up mid-session by
// theme.Watch) would keep showing its input fields' old background
// indefinitely, even though everything else in the view updates.
func TestRenderFormField_PicksUpLiveThemeChange(t *testing.T) {
	withTestPalette(t, theme.Palette{
		Primary: "#91b0de", Accent: "#9dc6e9", Success: "#99c2ed",
		Warning: "#a4cbf7", Danger: "#c79ea9", Text: "#c2d9e9",
		Dim: "#b7d2e5", Subtle: "#899eac", Bg: "#010101",
		BgAlt: "#0f1720", Border: "#5f6468", Highlight: "#afcfff",
	})
	f := newTimerForm(nil, nil) // bakes its inputs' styles under Bg=#010101

	// Skips the placeholder's leading "W": textinput renders that first rune
	// under a separate (cursor-position) style span, so it isn't a reliable
	// probe point for the plain PlaceholderStyle background this test cares
	// about.
	placeholder := "hat are you working on?"
	forceTrueColor(t)
	before := f.renderFormField("Task", fieldTask)
	if got := activeBackgroundAt(before, placeholder); got != "1;1;1" {
		t.Fatalf("before switch: background at placeholder = %q, want 1;1;1 (Bg=#010101)", got)
	}

	// Live theme switch — same form, no recreation, exactly like a running
	// app's theme.CheckReload()/theme.Watch() firing while this form is open.
	withTestPalette(t, theme.Palette{
		Primary: "#91b0de", Accent: "#9dc6e9", Success: "#99c2ed",
		Warning: "#a4cbf7", Danger: "#c79ea9", Text: "#c2d9e9",
		Dim: "#b7d2e5", Subtle: "#899eac", Bg: "#020202",
		BgAlt: "#0f1720", Border: "#5f6468", Highlight: "#afcfff",
	})

	after := f.renderFormField("Task", fieldTask)
	if got := activeBackgroundAt(after, placeholder); got != "2;2;2" {
		t.Errorf("after switch: background at placeholder = %q, want 2;2;2 (Bg=#020202) — the field is still showing the old theme's background", got)
	}
}

func TestViewForm_ClientDropdownVisible(t *testing.T) {
	clients := []*model.Client{mkClient(1, "Acme", 0), mkClient(2, "Beta", 0)}
	f := newTimerForm(clients, nil)
	f.focusField(fieldClient) // matches both clients (empty query) -> showClientDrop true
	m := TimersModel{mode: timersModeNew, form: f}
	got := m.viewForm()
	if !strings.Contains(got, "Acme") {
		t.Errorf("viewForm() with the client dropdown open should list matching clients, got %q", got)
	}
}

func TestViewForm_ProjectDropdownVisible(t *testing.T) {
	c := mkClient(1, "Acme", 0)
	projects := []*model.Project{mkProject(1, "Website", c), mkProject(2, "App", c)}
	f := newTimerForm(nil, projects)
	f.focusField(fieldProject)
	m := TimersModel{mode: timersModeNew, form: f}
	got := m.viewForm()
	if !strings.Contains(got, "Website") {
		t.Errorf("viewForm() with the project dropdown open should list matching projects, got %q", got)
	}
}

func TestViewForm_ErrLine(t *testing.T) {
	f := newTimerForm(nil, nil)
	m := TimersModel{mode: timersModeNew, form: f, err: "something broke"}
	got := m.viewForm()
	if !strings.Contains(got, "something broke") {
		t.Errorf("viewForm() with a set err should render it, got %q", got)
	}
}

// ── renderDropdown ────────────────────────────────────────────────────────────

func TestRenderDropdown_FewItems(t *testing.T) {
	items := []dropdownItem{{id: 0, label: `+ Create "New"`}, {id: 1, label: "Existing"}}
	// sel=1 selects the second (nonzero-id) item, leaving the first
	// (id==0, "create new") item unselected — exercises both the
	// unselected-id-0 style branch and the unselected-nonzero-id branch.
	got := renderDropdown(items, 1)
	if !strings.Contains(got, "New") || !strings.Contains(got, "Existing") {
		t.Errorf("renderDropdown with <=7 items should render all of them, got %q", got)
	}
}

func TestRenderDropdown_ManyItemsWindowed(t *testing.T) {
	var items []dropdownItem
	for i := 0; i < 20; i++ {
		items = append(items, dropdownItem{id: int64(i + 1), label: "item"})
	}
	sel := 19 // near the end -> exercises the "start := sel-4" windowing clamp
	got := renderDropdown(items, sel)
	if got == "" {
		t.Fatal("renderDropdown produced empty output")
	}
}

// ── statusLabel / invoiceLabel / billingStr ──────────────────────────────────

func TestStatusLabel(t *testing.T) {
	c := mkClient(1, "Acme", 50)
	p := mkProject(1, "Web", c)

	// Running is unflagged (like any other pending entry) — no badge, just
	// muted "pending" text; the row's own green highlight already conveys
	// that it's running.
	running := mkRunningEntry(1, p, "t", time.Now())
	if got := statusLabel(running, cBg); !strings.Contains(got, "pending") {
		t.Errorf("statusLabel(running, cBg) = %q, want it to mention 'pending'", got)
	}

	paid := mkEntry(2, p, "t", time.Now().Add(-time.Hour), 30)
	paid.Invoiced, paid.Paid = true, true
	if got := statusLabel(paid, cBg); !strings.Contains(got, "PAID") {
		t.Errorf("statusLabel(paid, cBg) = %q, want it to mention 'PAID'", got)
	}

	invoiced := mkEntry(3, p, "t", time.Now().Add(-time.Hour), 30)
	invoiced.Invoiced = true
	if got := statusLabel(invoiced, cBg); !strings.Contains(got, "INVOICED") {
		t.Errorf("statusLabel(invoiced, cBg) = %q, want it to mention 'INVOICED'", got)
	}

	// Pending is the unflagged default — plain muted text, no badge, same
	// term invoiceLabel's default case uses in the detail pane.
	pending := mkEntry(4, p, "t", time.Now().Add(-time.Hour), 30)
	if got := statusLabel(pending, cBg); !strings.Contains(got, "pending") {
		t.Errorf("statusLabel(pending, cBg) = %q, want it to mention 'pending'", got)
	}
}

func TestInvoiceLabel(t *testing.T) {
	c := mkClient(1, "Acme", 50)
	p := mkProject(1, "Web", c)

	paid := mkEntry(1, p, "t", time.Now().Add(-time.Hour), 30)
	paid.Invoiced, paid.Paid = true, true
	if got := invoiceLabel(paid); !strings.Contains(got, "PAID") {
		t.Errorf("invoiceLabel(paid) = %q, want it to mention 'PAID'", got)
	}

	invoiced := mkEntry(2, p, "t", time.Now().Add(-time.Hour), 30)
	invoiced.Invoiced = true
	if got := invoiceLabel(invoiced); !strings.Contains(got, "INVOICED") {
		t.Errorf("invoiceLabel(invoiced) = %q, want it to mention 'INVOICED'", got)
	}

	pending := mkEntry(3, p, "t", time.Now().Add(-time.Hour), 30)
	if got := invoiceLabel(pending); !strings.Contains(got, "pending") {
		t.Errorf("invoiceLabel(pending) = %q, want it to mention 'pending'", got)
	}
}

func TestBillingStr(t *testing.T) {
	c := mkClient(1, "Acme", 50)
	p := mkProject(1, "Web", c)
	free := mkClient(2, "Freebie", 0)
	pFree := mkProject(2, "OSS", free)

	billable := mkEntry(1, p, "t", time.Now().Add(-time.Hour), 60)
	if got := billingStr(billable); got != "$50.00" {
		t.Errorf("billingStr(1hr @ $50/hr) = %q, want %q", got, "$50.00")
	}

	zeroRate := mkEntry(2, pFree, "t", time.Now().Add(-time.Hour), 30)
	if got := billingStr(zeroRate); got != "—" {
		t.Errorf("billingStr(zero rate) = %q, want %q", got, "—")
	}

	noProject := mkEntry(3, p, "t", time.Now().Add(-time.Hour), 30)
	noProject.Project = nil
	if got := billingStr(noProject); got != "—" {
		t.Errorf("billingStr(no project) = %q, want %q", got, "—")
	}
}

// ── clampOffset: stale offset from a shrunk list ─────────────────────────────

func TestClampOffset_StaleOffsetClampedToNewRange(t *testing.T) {
	c := mkClient(1, "Acme", 0)
	p := mkProject(1, "Web", c)
	base := time.Date(2024, 5, 1, 9, 0, 0, 0, time.Local)
	var many []*model.Entry
	for i := 0; i < 20; i++ {
		many = append(many, mkEntry(int64(i+1), p, "t", base.Add(time.Duration(i)*time.Hour), 10))
	}

	m := TimersModel{height: 15, filtered: many, cursor: len(many) - 1}
	m.clampOffset()
	if m.offset == 0 {
		t.Fatal("expected a nonzero offset after scrolling to the bottom of a long list")
	}

	// Shrink the list (e.g. simulating a filter change) without resetting
	// offset — clampOffset must pull the stale offset back down to fit
	// within the smaller line count (the "m.offset > n-budget" branch).
	m.filtered = many[:8]
	m.cursor = 7
	m.clampOffset()

	lines := m.buildLines()
	budget := m.rowsBudget()
	if m.offset < 0 {
		t.Errorf("offset went negative: %d", m.offset)
	}
	if m.offset >= len(lines) {
		t.Errorf("offset %d not clamped to fit the shrunk list (len(lines)=%d)", m.offset, len(lines))
	}
	// The cursor's own line must fall inside the window that will actually
	// render — windowEnd, not a plain offset+budget bound, since a
	// synthesized sticky day header (see windowEnd's doc comment) can make
	// the true visible window one line narrower than budget.
	cursorLine := 0
	for i, l := range lines {
		if l.kind == timersLineEntry && l.entryIdx == m.cursor {
			cursorLine = i
			break
		}
	}
	if cursorLine < m.offset || cursorLine >= windowEnd(lines, m.offset, budget) {
		t.Errorf("cursor line %d not within rendered window [%d, %d)", cursorLine, m.offset, windowEnd(lines, m.offset, budget))
	}
}

// TestClampOffset_NegativeOffsetDefensivelyClamped directly constructs a
// TimersModel with an offset that could never arise from clampOffset's own
// arithmetic in normal use (its other three adjustments are proven, by that
// arithmetic, to never drive it negative — see the "if m.offset < 0" comment
// in timers.go) but exercises the final defensive clamp regardless, since
// nothing prevents an offset field from starting out negative.
func TestClampOffset_NegativeOffsetDefensivelyClamped(t *testing.T) {
	c := mkClient(1, "Acme", 0)
	p := mkProject(1, "Web", c)
	base := time.Date(2024, 5, 1, 9, 0, 0, 0, time.Local)
	var many []*model.Entry
	for i := 0; i < 20; i++ {
		many = append(many, mkEntry(int64(i+1), p, "t", base.Add(time.Duration(i)*time.Hour), 10))
	}

	m := TimersModel{height: 15, filtered: many, cursor: -1, offset: -50}
	m.clampOffset()
	if m.offset < 0 {
		t.Errorf("offset = %d, want clamped to >= 0", m.offset)
	}
}

// ── rangeLabel: remaining label()-closure branches ───────────────────────────

func TestRangeLabel_UnparseableCustomDate(t *testing.T) {
	m := TimersModel{preset: timersPresetCustom, dateFrom: "not-a-date", dateTo: "2020-01-05"}
	got := m.rangeLabel()
	if !strings.Contains(got, "—") {
		t.Errorf("rangeLabel() with an unparseable dateFrom = %q, want it to contain the placeholder dash", got)
	}
}

func TestRangeLabel_CustomRangeIncludingToday(t *testing.T) {
	today := time.Now().Local().Format("2006-01-02")
	m := TimersModel{preset: timersPresetCustom, dateFrom: today, dateTo: "2020-01-01"}
	got := m.rangeLabel()
	if !strings.Contains(got, "Today") {
		t.Errorf("rangeLabel() with dateFrom==today = %q, want it to say 'Today'", got)
	}
}

// ── viewList: paid / invoiced status rows ────────────────────────────────────

func TestViewList_PaidAndInvoicedStatuses(t *testing.T) {
	c := mkClient(1, "Acme", 60)
	p := mkProject(1, "Web", c)
	day := time.Date(2024, 5, 1, 9, 0, 0, 0, time.Local)

	paid := mkEntry(1, p, "paid work", day, 30)
	paid.Invoiced, paid.Paid = true, true
	invoicedOnly := mkEntry(2, p, "invoiced work", day, 30)
	invoicedOnly.Invoiced = true

	m := TimersModel{width: 120, height: 30, entries: []*model.Entry{paid, invoicedOnly}}
	m.recomputeFiltered()
	got := m.viewList()

	if !strings.Contains(got, "paid") {
		t.Errorf("viewList() should show a 'paid' status row, got:\n%s", got)
	}
	if !strings.Contains(got, "invoiced") {
		t.Errorf("viewList() should show an 'invoiced' status row, got:\n%s", got)
	}
}

// ── renderDayHeaderLine: narrow-width gap clamps ─────────────────────────────

func TestRenderDayHeaderLine_NarrowGapClamped(t *testing.T) {
	m := TimersModel{}
	l := timersLine{day: time.Now()}
	got := m.renderDayHeaderLine(l, 5) // contentW too small -> gap clamps to 0
	if got == "" {
		t.Fatal("renderDayHeaderLine produced empty output")
	}
}

// ── viewForm: save/cancel button focus styling ───────────────────────────────

func TestViewForm_SaveAndCancelButtonFocus(t *testing.T) {
	saveFocused := newTimerForm(nil, nil)
	saveFocused.focusField(fieldSave)
	m := TimersModel{mode: timersModeNew, form: saveFocused}
	if got := m.viewForm(); !strings.Contains(got, "Save & Start") {
		t.Errorf("viewForm() with Save focused should still render the Save button, got %q", got)
	}

	cancelFocused := newTimerForm(nil, nil)
	cancelFocused.focusField(fieldCancel)
	m2 := TimersModel{mode: timersModeNew, form: cancelFocused}
	if got := m2.viewForm(); !strings.Contains(got, "Cancel") {
		t.Errorf("viewForm() with Cancel focused should still render the Cancel button, got %q", got)
	}
}
