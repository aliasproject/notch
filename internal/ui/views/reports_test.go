package views

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aliasproject/notch/internal/model"
	tea "github.com/charmbracelet/bubbletea"
)

func mkReportRow(clientName, projectName string, hours, earnings float64, entryCount, invoiced, paid int) *model.ReportRow {
	return &model.ReportRow{
		ClientName: clientName, ProjectName: projectName,
		TotalHours: hours, Earnings: earnings,
		EntryCount: entryCount, Invoiced: invoiced, Paid: paid,
	}
}

// ── renderTotals / renderSummaryCards aggregation ────────────────────────────

func TestRenderTotals_SumsAcrossRows(t *testing.T) {
	m := ReportsModel{width: 140, rows: []*model.ReportRow{
		mkReportRow("Acme", "Website", 2, 200, 2, 2, 1),
		mkReportRow("Beta", "App", 3, 300, 3, 1, 0),
	}}

	got := m.renderTotals()
	if !strings.Contains(got, "5.00h") {
		t.Errorf("renderTotals() should show total hours 5.00h, got: %q", got)
	}
	if !strings.Contains(got, "$500.00") {
		t.Errorf("renderTotals() should show total earnings $500.00, got: %q", got)
	}
	if !strings.Contains(got, "3/5") {
		t.Errorf("renderTotals() should show invoiced 3/5, got: %q", got)
	}
	if !strings.Contains(got, "1/5") {
		t.Errorf("renderTotals() should show paid 1/5, got: %q", got)
	}
}

func TestRenderSummaryCards_OutstandingExcludesFullyPaidRows(t *testing.T) {
	// Row 1 is fully paid (Paid == EntryCount): its earnings should be excluded
	// from "Outstanding". Row 2 is only partially paid, so its full earnings count.
	m := ReportsModel{width: 140, rows: []*model.ReportRow{
		mkReportRow("Acme", "Website", 2, 200, 2, 2, 2), // fully paid
		mkReportRow("Beta", "App", 3, 300, 3, 3, 1),     // invoiced, partially paid
	}}

	got := m.renderSummaryCards(5, 500, 5, 5, 3)
	if !strings.Contains(got, "$300.00") {
		t.Errorf("renderSummaryCards() Outstanding should be $300.00 (Beta's earnings only), got: %q", got)
	}
	// unpaidEntries = entries(5) - paid(3) = 2
	if !strings.Contains(got, "2 entries") {
		t.Errorf("renderSummaryCards() Uninvoiced should show 2 entries, got: %q", got)
	}
}

func TestRenderSummaryCards_NoRows(t *testing.T) {
	m := ReportsModel{width: 140}
	got := m.renderSummaryCards(0, 0, 0, 0, 0)
	if !strings.Contains(got, "0.00 h") {
		t.Errorf("renderSummaryCards() with no rows should show 0.00 h, got: %q", got)
	}
	if !strings.Contains(got, "$0.00") {
		t.Errorf("renderSummaryCards() with no rows should show $0.00 earned, got: %q", got)
	}
}

// ── mainColWidths ─────────────────────────────────────────────────────────────

func TestMainColWidths_SpansAvailableWidth(t *testing.T) {
	m := ReportsModel{width: 140}
	clientW, projectW := m.mainColWidths()
	if clientW <= 0 || projectW <= 0 {
		t.Errorf("mainColWidths() = (%d, %d), want both positive", clientW, projectW)
	}
	total := usableWidth(140) - rColGutter - rColHours - rColEntries - rColInv - rColPaid - rColEarned
	if clientW+projectW != total {
		t.Errorf("clientW+projectW = %d, want %d (all remaining space used)", clientW+projectW, total)
	}
}

func TestMainColWidths_NeverNegative(t *testing.T) {
	m := ReportsModel{width: 5} // far too narrow for even the fixed columns
	clientW, projectW := m.mainColWidths()
	if clientW < 0 || projectW < 0 {
		t.Errorf("mainColWidths() = (%d, %d), want non-negative even at tiny widths", clientW, projectW)
	}
}

// ── View ──────────────────────────────────────────────────────────────────────

// TestView_FilterModeHidesTable is a regression test: View() used to show
// the filter form *and* the (soon-to-be-stale) table underneath it at the
// same time, pushing the table down the page. It should now fully replace
// the body while filtering — just header + filter form — matching
// TimersModel.viewList's identical branch, and restore the table once
// filtering ends.
func TestView_FilterModeHidesTable(t *testing.T) {
	m := NewReports(nil)
	m.width, m.height = 100, 30
	m.rows = []*model.ReportRow{mkReportRow("Acme Corp", "Website Redesign", 2, 200, 2, 1, 0)}

	m.mode = reportsModeFilter
	filtering := m.View()
	if !strings.Contains(filtering, "From") || !strings.Contains(filtering, "To") {
		t.Errorf("View() in filter mode should show the date-range filter bar, got:\n%s", filtering)
	}
	// "Website Redesign" gets column-truncated to "Website Red…" in the
	// table, so check for a prefix that survives that rather than the full
	// name.
	if strings.Contains(filtering, "Acme Corp") || strings.Contains(filtering, "Website Red") {
		t.Errorf("View() in filter mode should hide the table, not push it down below the filter bar, got:\n%s", filtering)
	}

	m.mode = reportsModeView
	normal := m.View()
	if !strings.Contains(normal, "Acme Corp") || !strings.Contains(normal, "Website Red") {
		t.Errorf("View() outside filter mode should show the table again, got:\n%s", normal)
	}
}

// ── Filter-mode transitions ──────────────────────────────────────────────────

func TestReportsUpdateView_FilterKeyEntersFilterMode(t *testing.T) {
	m := NewReports(nil)
	got, cmd := m.updateView(runeKey('f'))
	if got.mode != reportsModeFilter {
		t.Errorf("mode after 'f' = %v, want reportsModeFilter", got.mode)
	}
	if cmd == nil {
		t.Error("want a non-nil cmd (textinput.Blink) after entering filter mode")
	}
}

func TestReportsUpdateFilter_ApplyCommitsDates(t *testing.T) {
	m := NewReports(nil)
	m.mode = reportsModeFilter
	m.fromInput.SetValue("2024-01-01")
	m.toInput.SetValue("2024-01-31")

	got, cmd := m.updateFilter(tea.KeyMsg{Type: tea.KeyEnter})
	if got.mode != reportsModeView {
		t.Errorf("mode after apply = %v, want reportsModeView", got.mode)
	}
	if got.dateFrom != "2024-01-01" || got.dateTo != "2024-01-31" {
		t.Errorf("dateFrom/dateTo = %q/%q, want 2024-01-01/2024-01-31", got.dateFrom, got.dateTo)
	}
	if cmd == nil {
		t.Error("want a non-nil loadCmd after applying the filter")
	}
}

func TestReportsUpdateFilter_EscCancelsWithoutCommitting(t *testing.T) {
	m := NewReports(nil)
	m.mode = reportsModeFilter
	m.dateFrom, m.dateTo = "2024-06-01", "2024-06-30"
	m.fromInput.SetValue("garbage")

	got, cmd := m.updateFilter(tea.KeyMsg{Type: tea.KeyEsc})
	if got.mode != reportsModeView {
		t.Errorf("mode after esc = %v, want reportsModeView", got.mode)
	}
	if got.dateFrom != "2024-06-01" || got.dateTo != "2024-06-30" {
		t.Errorf("dates should be untouched on cancel, got %q/%q", got.dateFrom, got.dateTo)
	}
	if cmd != nil {
		t.Error("want nil cmd on cancel")
	}
}

func TestReportsUpdateFilter_TabTogglesFocus(t *testing.T) {
	m := NewReports(nil)
	m.mode = reportsModeFilter
	m.filterIdx = 0

	got, _ := m.updateFilter(tea.KeyMsg{Type: tea.KeyTab})
	if got.filterIdx != 1 {
		t.Errorf("filterIdx after tab = %d, want 1", got.filterIdx)
	}

	got2, _ := got.updateFilter(tea.KeyMsg{Type: tea.KeyShiftTab})
	if got2.filterIdx != 0 {
		t.Errorf("filterIdx after shift+tab = %d, want back to 0", got2.filterIdx)
	}
}

// ── Preset keys (each issues a reload cmd) ───────────────────────────────────

func TestReportsUpdateView_PresetKeysSetDateRange(t *testing.T) {
	m := NewReports(nil)

	got, cmd := m.updateView(runeKey('a')) // "all time"
	if got.dateFrom != "" || got.dateTo != "" {
		t.Errorf("dateFrom/dateTo after 'a' = %q/%q, want both empty", got.dateFrom, got.dateTo)
	}
	if cmd == nil {
		t.Error("want a non-nil loadCmd after changing the preset")
	}

	got2, _ := m.updateView(runeKey('t')) // "today"
	if got2.dateFrom == "" || got2.dateFrom != got2.dateTo {
		t.Errorf("dateFrom/dateTo after 't' = %q/%q, want equal non-empty dates", got2.dateFrom, got2.dateTo)
	}
}

// ── Help ──────────────────────────────────────────────────────────────────────

// TestHelp_MentionsDrillDown is a regression test for a discoverability
// gap: View() always shows the selected row's underlying entries below the
// table (no dedicated key — it's automatic, driven by cursor position, see
// the "Drill-down" comment in View()), but the old hotkey text just said
// "↑/↓ navigate", giving no hint that navigating also reveals that. It
// should call out what up/down actually does here, not just that they move
// the cursor.
func TestHelp_MentionsDrillDown(t *testing.T) {
	m := ReportsModel{mode: reportsModeView}
	got := m.Help()
	found := false
	for _, h := range got {
		if strings.Contains(h.Label, "entries") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Help() = %v, want it to mention that ↑/↓ reveals the row's entries", got)
	}
}

// ── loadCmd / entries drill-down ─────────────────────────────────────────────

// TestLoadCmd_PopulatesEntries is a regression test: ReportByProject only
// aggregates (SUM/COUNT via GROUP BY) — it never attached the underlying
// entries, so renderEntries' drill-down had nothing to show for any row,
// no matter how you navigated. loadCmd should now fetch and attach each
// row's entries itself.
func TestLoadCmd_PopulatesEntries(t *testing.T) {
	d := newTestDB(t)
	c, err := d.CreateClient("Acme", 100)
	if err != nil {
		t.Fatal(err)
	}
	p, err := d.CreateProject(c.ID, "Website")
	if err != nil {
		t.Fatal(err)
	}
	e, err := d.StartEntry(p.ID, "build feature")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.StopEntry(e.ID); err != nil {
		t.Fatal(err)
	}

	m := ReportsModel{db: d}
	msg := m.loadCmd()()
	rows, ok := msg.(reportRowsMsg)
	if !ok {
		t.Fatalf("loadCmd() produced %T, want reportRowsMsg", msg)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if len(rows[0].Entries) != 1 {
		t.Fatalf("rows[0].Entries = %+v, want 1 entry", rows[0].Entries)
	}
	if rows[0].Entries[0].Task != "build feature" {
		t.Errorf("rows[0].Entries[0].Task = %q, want %q", rows[0].Entries[0].Task, "build feature")
	}
}

func TestLoadCmd_Error(t *testing.T) {
	d := newTestDB(t)
	d.Close()
	m := ReportsModel{db: d}
	msg := m.loadCmd()()
	if _, ok := msg.(ErrMsg); !ok {
		t.Errorf("loadCmd() on a closed db produced %T, want ErrMsg", msg)
	}
}

// ── Table windowing ───────────────────────────────────────────────────────────

// TestClampOffset_Reports_ScrollsSelectedRowIntoView checks that a report
// with many client/project rows scrolls the table (rather than overflowing
// the screen) to keep the selected row visible — mirroring
// TimersModel.viewList's identical windowing for its own list.
func TestClampOffset_Reports_ScrollsSelectedRowIntoView(t *testing.T) {
	var rows []*model.ReportRow
	for i := 0; i < 20; i++ {
		rows = append(rows, mkReportRow("Acme", fmt.Sprintf("Project %02d", i), 1, 0, 1, 0, 0))
	}

	m := ReportsModel{width: 120, height: 20, rows: rows, cursor: 19}
	m.clampOffset()

	budget := m.rowsBudget()
	if budget < 1 {
		t.Fatalf("rowsBudget() = %d, want >= 1", budget)
	}
	if m.cursor < m.offset || m.cursor >= m.offset+budget {
		t.Errorf("cursor %d not within visible window [%d, %d) after clampOffset", m.cursor, m.offset, m.offset+budget)
	}
}

// ── Entries mode: transitions ─────────────────────────────────────────────────

func TestUpdateView_EnterOpensEntriesMode(t *testing.T) {
	rows := []*model.ReportRow{mkReportRow("Acme", "Web", 1, 100, 1, 0, 0)}
	m := ReportsModel{mode: reportsModeView, rows: rows, cursor: 0, entriesOffset: 3}

	got, _ := m.updateView(tea.KeyMsg{Type: tea.KeyEnter})
	if got.mode != reportsModeEntries {
		t.Errorf("mode after enter = %v, want reportsModeEntries", got.mode)
	}
	if got.entriesOffset != 0 {
		t.Errorf("entriesOffset after opening = %d, want reset to 0", got.entriesOffset)
	}
}

func TestUpdateView_EnterWithNoRowsIsNoOp(t *testing.T) {
	m := ReportsModel{mode: reportsModeView}
	got, _ := m.updateView(tea.KeyMsg{Type: tea.KeyEnter})
	if got.mode != reportsModeView {
		t.Errorf("mode after enter with no rows = %v, want to stay reportsModeView", got.mode)
	}
}

func TestUpdateEntries_EscGoesBack(t *testing.T) {
	m := ReportsModel{mode: reportsModeEntries}
	got, _ := m.updateEntries(tea.KeyMsg{Type: tea.KeyEsc})
	if got.mode != reportsModeView {
		t.Errorf("mode after esc = %v, want reportsModeView", got.mode)
	}
}

func TestUpdateEntries_ScrollsWithinBounds(t *testing.T) {
	c := mkClient(1, "Acme", 100)
	p := mkProject(1, "Web", c)
	r := mkReportRow("Acme", "Web", 1, 100, 10, 0, 0)
	for i := 0; i < 10; i++ {
		r.Entries = append(r.Entries, mkEntry(int64(i+1), p, "t", time.Now(), 10))
	}
	m := ReportsModel{mode: reportsModeEntries, width: 120, height: 15, rows: []*model.ReportRow{r}, cursor: 0}

	// Up at offset 0 stays at 0 (no underflow).
	got, _ := m.updateEntries(runeKey('k'))
	if got.entriesOffset != 0 {
		t.Errorf("entriesOffset after up at 0 = %d, want 0", got.entriesOffset)
	}

	// Down repeatedly must not exceed what clampEntriesOffset allows.
	for i := 0; i < 20; i++ {
		got, _ = got.updateEntries(runeKey('j'))
	}
	budget := got.entriesRowsBudget()
	maxOffset := len(r.Entries) - budget
	if maxOffset < 0 {
		maxOffset = 0
	}
	if got.entriesOffset > maxOffset {
		t.Errorf("entriesOffset = %d after scrolling past the end, want clamped to <= %d", got.entriesOffset, maxOffset)
	}
}

// ── renderEntriesView ────────────────────────────────────────────────────────

// TestRenderEntriesView_AllEntriesReachableByScrolling is a regression test
// replacing the old capped inline drill-down: with many entries and a
// short view, not everything fits in one screen, but every entry should
// still be reachable by scrolling (via entriesOffset) rather than
// permanently truncated behind a "… and N more" note with no way to see
// the rest.
func TestRenderEntriesView_AllEntriesReachableByScrolling(t *testing.T) {
	c := mkClient(1, "Acme", 100)
	p := mkProject(1, "Web", c)
	base := time.Date(2024, 5, 1, 9, 0, 0, 0, time.Local)

	r := &model.ReportRow{ClientName: "Acme", ProjectName: "Web", HourlyRate: 100}
	const total = 15
	for i := 0; i < total; i++ {
		r.Entries = append(r.Entries, mkEntry(int64(i+1), p, fmt.Sprintf("task %d", i), base.Add(time.Duration(i)*time.Hour), 10))
	}

	m := ReportsModel{width: 120, height: 15, rows: []*model.ReportRow{r}, cursor: 0}
	budget := m.entriesRowsBudget()
	if budget >= total {
		t.Fatalf("test setup invalid: budget %d must be smaller than %d entries to exercise scrolling", budget, total)
	}

	seen := map[string]bool{}
	for m.entriesCursor = 0; m.entriesCursor < total; m.entriesCursor++ {
		m.clampEntriesOffset()
		got := m.renderEntriesView()
		for i := 0; i < total; i++ {
			task := fmt.Sprintf("task %d", i)
			if strings.Contains(got, task) {
				seen[task] = true
			}
		}
	}
	if len(seen) != total {
		t.Errorf("scrolled through every entry but only saw %d/%d distinct tasks", len(seen), total)
	}
}

func TestRenderEntriesView_NoEntries(t *testing.T) {
	r := &model.ReportRow{ClientName: "Acme", ProjectName: "Web"}
	m := ReportsModel{width: 120, height: 20, rows: []*model.ReportRow{r}, cursor: 0}
	got := m.renderEntriesView()
	if !strings.Contains(got, "No entries") {
		t.Errorf("renderEntriesView with no entries = %q, want it to say so", got)
	}
}

func TestRenderEntriesView_NoSelection(t *testing.T) {
	m := ReportsModel{width: 120, height: 20}
	got := m.renderEntriesView()
	if got == "" {
		t.Error("renderEntriesView with no selection should still render (falls back to the header), got empty string")
	}
}

// TestEntryCols_RowWidthMatchesDivider is a regression test: entryCols used
// to subtract a hardcoded 8 for the status column's width instead of the
// actual 12, leaving every entry row (and the table header) 4 columns wider
// than totalW/the divider. Since app.go's outer column wraps the whole view
// in Width(cw), and lipgloss word-wraps (never truncates) content wider
// than that, every over-width row silently wrapped onto an extra line —
// doubling the effective table height, overflowing well past
// entriesRowsBudget, and pushing the header off the top of the screen in a
// real terminal (a headless string-only render doesn't show this, since
// nothing there re-wraps the way the live app's outer Width(cw) does).
func TestEntryCols_RowWidthMatchesDivider(t *testing.T) {
	for _, w := range []int{80, 100, 120, 150, 200} {
		m := ReportsModel{width: w}
		date, task, dur, earned, status := m.entryCols()
		gotRowWidth := 2 + date + task + dur + earned + status // 2 = RowPrefix gutter
		wantRowWidth := usableWidth(w)
		if gotRowWidth != wantRowWidth {
			t.Errorf("width=%d: entry row width = %d, want exactly %d (usableWidth) — any mismatch triggers unwanted word-wrap", w, gotRowWidth, wantRowWidth)
		}
	}
}

func TestView_EntriesMode(t *testing.T) {
	rows := []*model.ReportRow{mkReportRow("Acme", "Web", 1, 100, 1, 0, 0)}
	rows[0].Entries = []*model.Entry{mkEntry(1, mkProject(1, "Web", mkClient(1, "Acme", 100)), "build feature", time.Now(), 30)}

	m := ReportsModel{mode: reportsModeEntries, width: 120, height: 20, rows: rows, cursor: 0}
	got := m.View()
	if !strings.Contains(got, "Acme › Web") {
		t.Errorf("View() in entries mode should show the selected row's title, got:\n%s", got)
	}
	if !strings.Contains(got, "build feature") {
		t.Errorf("View() in entries mode should show its entries, got:\n%s", got)
	}
}

func TestHelp_EntriesMode(t *testing.T) {
	m := ReportsModel{mode: reportsModeEntries}
	got := m.Help()
	found := false
	for _, h := range got {
		if h.Key == "esc" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Help() in entries mode = %v, want it to mention esc to go back", got)
	}
}

func TestIsBusy_EntriesMode(t *testing.T) {
	m := ReportsModel{mode: reportsModeEntries}
	if !m.IsBusy() {
		t.Error("IsBusy() in entries mode should be true — it should capture all keystrokes like filter mode does")
	}
}
