package views

import (
	"fmt"
	"strings"
	"time"

	"github.com/aliasproject/notch/internal/db"
	"github.com/aliasproject/notch/internal/model"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Column widths ─────────────────────────────────────────────────────────────

const (
	rColGutter  = 2
	rColHours   = 12
	rColEntries = 12
	rColInv     = 12
	rColPaid    = 12
	rColEarned  = 14
)

type reportsMode int

const (
	reportsModeView reportsMode = iota
	reportsModeFilter
	reportsModeEntries
	reportsModeEditEntry
)

type ReportsModel struct {
	db     *db.DB
	width  int
	height int
	mode   reportsMode

	rows   []*model.ReportRow
	cursor int
	offset int // index into rows; see clampOffset

	entriesCursor int // index into the selected row's entries, in reportsModeEntries; see clampEntriesOffset
	entriesOffset int // scroll offset within entries; see clampEntriesOffset

	// editForm/editFormErr back reportsModeEditEntry — populated from
	// reportsOpenEditMsg, driven by updateTimerFormFields/viewTimerForm
	// (shared with TimersModel's own form; see updateTimerFormFields' doc
	// comment in timers.go for why those are free functions rather than
	// TimersModel methods).
	editForm    timerForm
	editFormErr string

	fromInput textinput.Model
	toInput   textinput.Model
	filterIdx int

	dateFrom string
	dateTo   string
	err      string
}

func NewReports(database *db.DB) ReportsModel {
	// makeTextInput (not a bare textinput.New()) so these pick up the same
	// background/prompt/cursor styling and width-safety margin as Timers'
	// identical filter — see its doc comment and dateFilterInputWidth in
	// common.go.
	from := makeTextInput("YYYY-MM-DD", 10, dateFilterInputWidth)
	to := makeTextInput("YYYY-MM-DD", 10, dateFilterInputWidth)

	// Default: current month
	now := time.Now()
	defaultFrom := fmt.Sprintf("%d-%02d-01", now.Year(), now.Month())
	defaultTo := now.Format("2006-01-02")
	from.SetValue(defaultFrom)
	to.SetValue(defaultTo)

	return ReportsModel{
		db:        database,
		fromInput: from,
		toInput:   to,
		dateFrom:  defaultFrom,
		dateTo:    defaultTo,
	}
}

func (m ReportsModel) Init() tea.Cmd {
	return m.loadCmd()
}

// IsBusy returns true when the view is in a mode that should capture all keystrokes.
func (m ReportsModel) IsBusy() bool {
	return m.mode == reportsModeFilter || m.mode == reportsModeEntries || m.mode == reportsModeEditEntry
}

// Help returns the contextual hotkey string for the view's current mode, for
// display in the app-level hotkey bar (see app.go renderHotkeyBar).
func (m ReportsModel) Help() []Hotkey {
	switch m.mode {
	case reportsModeFilter:
		return []Hotkey{{"tab", "switch"}, {"enter", "apply"}, {"esc", "cancel"}}
	case reportsModeEntries:
		return []Hotkey{{"↑ / ↓", "navigate"}, {"e", "edit"}, {"esc", "back"}}
	case reportsModeEditEntry:
		return []Hotkey{
			{"tab / shift+tab", "move"},
			{"↑ / ↓", "dropdown"},
			{"enter", "confirm"},
			{"esc", "cancel"},
		}
	default:
		return []Hotkey{
			{"↑ / ↓", "navigate"},
			{"enter", "view entries"},
			{"f", "filter"},
			{"t", "today"},
			{"w", "week"},
			{"m", "month"},
			{"y", "year"},
			{"a", "all"},
		}
	}
}

func (m *ReportsModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// -- messages -----------------------------------------------------------------

type reportRowsMsg []*model.ReportRow

func (m ReportsModel) loadCmd() tea.Cmd {
	from := m.dateFrom
	to := m.dateTo
	return func() tea.Msg {
		rows, err := m.db.ReportByProject(from, to)
		if err != nil {
			return ErrMsg(err.Error())
		}
		// ReportByProject only aggregates (SUM/COUNT via GROUP BY) — it
		// never attaches the underlying entries, so renderEntries' drill-
		// down had nothing to show no matter which row was selected.
		// Fetching them eagerly here (rather than lazily per selection)
		// keeps the report's loading model simple: one command, one
		// message, and report row counts are small enough (a handful of
		// client/project combos, typically) that this isn't costly.
		// includeRunning=false matches ReportByProject's own "e.end_time IS
		// NOT NULL" filter, so a row's EntryCount/TotalHours always agree
		// with how many entries actually show here.
		for _, r := range rows {
			entries, err := m.db.ListEntries(r.ProjectID, from, to, false)
			if err != nil {
				return ErrMsg(err.Error())
			}
			r.Entries = entries
		}
		return reportRowsMsg(rows)
	}
}

// -- Update -------------------------------------------------------------------

type reportsKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Filter key.Binding
	Apply  key.Binding
	Edit   key.Binding
	Cancel key.Binding
	Today  key.Binding
	Week   key.Binding
	Month  key.Binding
	Year   key.Binding
	All    key.Binding
	Next   key.Binding
	Prev   key.Binding
}

var reportsKeys = reportsKeyMap{
	Up:     key.NewBinding(key.WithKeys("up", "k")),
	Down:   key.NewBinding(key.WithKeys("down", "j")),
	Filter: key.NewBinding(key.WithKeys("f")),
	Apply:  key.NewBinding(key.WithKeys("enter")),
	Edit:   key.NewBinding(key.WithKeys("e")),
	Cancel: key.NewBinding(key.WithKeys("esc")),
	Today:  key.NewBinding(key.WithKeys("t")),
	Week:   key.NewBinding(key.WithKeys("w")),
	Month:  key.NewBinding(key.WithKeys("m")),
	Year:   key.NewBinding(key.WithKeys("y")),
	All:    key.NewBinding(key.WithKeys("a")),
	Next:   key.NewBinding(key.WithKeys("tab")),
	Prev:   key.NewBinding(key.WithKeys("shift+tab")),
}

func (m ReportsModel) Update(msg tea.Msg) (ReportsModel, tea.Cmd) {
	switch msg := msg.(type) {

	case reportRowsMsg:
		m.rows = []*model.ReportRow(msg)
		if m.cursor >= len(m.rows) && m.cursor > 0 {
			m.cursor = len(m.rows) - 1
		}
		m.clampOffset()
		m.err = ""
		return m, nil

	case reportsOpenEditMsg:
		return m.handleOpenEditEntry(msg), nil

	case tea.KeyMsg:
		switch m.mode {
		case reportsModeFilter:
			return m.updateFilter(msg)
		case reportsModeEntries:
			return m.updateEntries(msg)
		case reportsModeEditEntry:
			return m.updateEditEntry(msg)
		default:
			return m.updateView(msg)
		}
	}
	return m, nil
}

func (m ReportsModel) updateView(msg tea.KeyMsg) (ReportsModel, tea.Cmd) {
	switch {
	case key.Matches(msg, reportsKeys.Up):
		if m.cursor > 0 {
			m.cursor--
			m.clampOffset()
		}
	case key.Matches(msg, reportsKeys.Down):
		if m.cursor < len(m.rows)-1 {
			m.cursor++
			m.clampOffset()
		}
	case key.Matches(msg, reportsKeys.Apply):
		// Opens the selected row's full entries view — its own dedicated
		// screen (matching how editing a timer or filtering by date fully
		// replaces the view), rather than a capped inline preview
		// competing with this table for space. See renderEntriesView.
		if m.cursor >= 0 && m.cursor < len(m.rows) {
			m.mode = reportsModeEntries
			m.entriesCursor = 0
			m.entriesOffset = 0
		}
	case key.Matches(msg, reportsKeys.Filter):
		m.mode = reportsModeFilter
		m.filterIdx = 0
		m.fromInput.Focus()
		m.toInput.Blur()
		return m, textinput.Blink
	case key.Matches(msg, reportsKeys.Today):
		today := time.Now().Format("2006-01-02")
		m.dateFrom = today
		m.dateTo = today
		m.fromInput.SetValue(today)
		m.toInput.SetValue(today)
		return m, m.loadCmd()
	case key.Matches(msg, reportsKeys.Week):
		now := time.Now()
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		monday := now.AddDate(0, 0, -(weekday - 1))
		m.dateFrom = monday.Format("2006-01-02")
		m.dateTo = now.Format("2006-01-02")
		m.fromInput.SetValue(m.dateFrom)
		m.toInput.SetValue(m.dateTo)
		return m, m.loadCmd()
	case key.Matches(msg, reportsKeys.Month):
		now := time.Now()
		m.dateFrom = fmt.Sprintf("%d-%02d-01", now.Year(), now.Month())
		m.dateTo = now.Format("2006-01-02")
		m.fromInput.SetValue(m.dateFrom)
		m.toInput.SetValue(m.dateTo)
		return m, m.loadCmd()
	case key.Matches(msg, reportsKeys.Year):
		now := time.Now()
		m.dateFrom = fmt.Sprintf("%d-01-01", now.Year())
		m.dateTo = now.Format("2006-01-02")
		m.fromInput.SetValue(m.dateFrom)
		m.toInput.SetValue(m.dateTo)
		return m, m.loadCmd()
	case key.Matches(msg, reportsKeys.All):
		m.dateFrom = ""
		m.dateTo = ""
		m.fromInput.SetValue("")
		m.toInput.SetValue("")
		return m, m.loadCmd()
	}
	return m, nil
}

// updateEntries handles input while viewing a single row's full entries
// list (reportsModeEntries): ↑/↓ moves the selection (auto-scrolling to
// keep it visible, same shape as TimersModel's own list — see
// clampEntriesOffset), 'e' opens the selected entry for editing, esc goes
// back to the report table.
func (m ReportsModel) updateEntries(msg tea.KeyMsg) (ReportsModel, tea.Cmd) {
	r := m.selectedRow()
	switch {
	case key.Matches(msg, reportsKeys.Cancel):
		m.mode = reportsModeView
	case key.Matches(msg, reportsKeys.Up):
		if m.entriesCursor > 0 {
			m.entriesCursor--
			m.clampEntriesOffset()
		}
	case key.Matches(msg, reportsKeys.Down):
		if r != nil && m.entriesCursor < len(r.Entries)-1 {
			m.entriesCursor++
			m.clampEntriesOffset()
		}
	case key.Matches(msg, reportsKeys.Edit):
		if e := m.selectedEntry(); e != nil {
			return m, m.openEditEntryCmd(e.ID)
		}
	}
	return m, nil
}

// updateEditEntry handles input while editing a single entry
// (reportsModeEditEntry). Field navigation, dropdowns, and typing all
// delegate to updateTimerFormFields — the same logic TimersModel's own
// New/Edit Timer form uses — so only Cancel/Save need handling here.
func (m ReportsModel) updateEditEntry(msg tea.KeyMsg) (ReportsModel, tea.Cmd) {
	action, cmd := updateTimerFormFields(&m.editForm, msg)
	switch action {
	case "cancel":
		m.mode = reportsModeEntries
		m.editFormErr = ""
		return m, nil
	case "submit":
		return m.submitEditEntry()
	}
	return m, cmd
}

// submitEditEntry validates the edit form and, if valid, writes the change
// and reloads this report's rows so the table/entries list reflects it —
// the Reports-side counterpart to TimersModel.submitForm's edit branch,
// sharing the same validateTimerForm/submitEditEntryCmd.
func (m ReportsModel) submitEditEntry() (ReportsModel, tea.Cmd) {
	v, errMsg := validateTimerForm(m.editForm)
	if errMsg != "" {
		m.editFormErr = errMsg
		return m, nil
	}
	m.mode = reportsModeEntries
	m.editFormErr = ""
	return m, submitEditEntryCmd(m.db, m.editForm.entryID, v, m.loadCmd())
}

// reportsOpenEditMsg carries the data needed to populate the edit-entry
// form for a specific entry. It's a distinct type from Timers'
// openFormMsg deliberately: app.go broadcasts every non-key tea.Msg to
// *all* tabs (not just the active one — see its Update comment "Everything
// else → all tabs"), so reusing openFormMsg here would also pop open the
// real, currently-inactive Timers tab's own edit form for the same entry.
type reportsOpenEditMsg struct {
	entryID  int64
	clients  []*model.Client
	projects []*model.Project
}

// openEditEntryCmd fetches the clients/projects needed to populate the
// edit form for entryID — mirrors TimersModel.openFormCmd, but returns
// reportsOpenEditMsg instead of the shared openFormMsg (see its doc
// comment for why).
func (m ReportsModel) openEditEntryCmd(entryID int64) tea.Cmd {
	return func() tea.Msg {
		clients, err := m.db.ListClients()
		if err != nil {
			return ErrMsg(err.Error())
		}
		projects, err := m.db.ListProjects(0)
		if err != nil {
			return ErrMsg(err.Error())
		}
		return reportsOpenEditMsg{entryID: entryID, clients: clients, projects: projects}
	}
}

// handleOpenEditEntry populates m.editForm from the entry named in msg and
// switches to reportsModeEditEntry — mirrors
// TimersModel.handleOpenForm's population logic exactly (same field
// mapping), since it's populating the same kind of form.
func (m ReportsModel) handleOpenEditEntry(msg reportsOpenEditMsg) ReportsModel {
	e := m.entryByID(msg.entryID)
	if e == nil {
		m.err = "Entry not found"
		return m
	}

	f := newTimerForm(msg.clients, msg.projects)
	f.entryID = e.ID
	f.inputs[fieldTask].SetValue(e.Task)
	f.inputs[fieldNotes].SetValue(e.Notes)
	f.inputs[fieldStart].SetValue(e.StartTime.Local().Format("2006-01-02 15:04"))
	if e.EndTime != nil {
		f.inputs[fieldEnd].SetValue(e.EndTime.Local().Format("2006-01-02 15:04"))
	}
	if e.Project != nil {
		f.projectID = e.Project.ID
		f.inputs[fieldProject].SetValue(e.Project.Name)
		if e.Project.Client != nil {
			f.clientID = e.Project.Client.ID
			f.inputs[fieldClient].SetValue(e.Project.Client.Name)
		}
	}
	f.rebuildClientMatches()
	f.rebuildProjectMatches()

	m.editForm = f
	m.mode = reportsModeEditEntry
	m.editFormErr = ""
	return m
}

func (m ReportsModel) updateFilter(msg tea.KeyMsg) (ReportsModel, tea.Cmd) {
	switch {
	case key.Matches(msg, reportsKeys.Cancel):
		m.mode = reportsModeView
		m.fromInput.Blur()
		m.toInput.Blur()
		return m, nil

	case key.Matches(msg, reportsKeys.Apply):
		m.dateFrom = strings.TrimSpace(m.fromInput.Value())
		m.dateTo = strings.TrimSpace(m.toInput.Value())
		m.mode = reportsModeView
		m.fromInput.Blur()
		m.toInput.Blur()
		return m, m.loadCmd()

	case key.Matches(msg, reportsKeys.Next):
		m.fromInput.Blur()
		m.toInput.Blur()
		m.filterIdx = (m.filterIdx + 1) % 2
		if m.filterIdx == 0 {
			m.fromInput.Focus()
		} else {
			m.toInput.Focus()
		}
		return m, textinput.Blink

	case key.Matches(msg, reportsKeys.Prev):
		m.fromInput.Blur()
		m.toInput.Blur()
		m.filterIdx = (m.filterIdx - 1 + 2) % 2
		if m.filterIdx == 0 {
			m.fromInput.Focus()
		} else {
			m.toInput.Focus()
		}
		return m, textinput.Blink
	}

	// Forward to focused input
	var cmd tea.Cmd
	if m.filterIdx == 0 {
		m.fromInput, cmd = m.fromInput.Update(msg)
	} else {
		m.toInput, cmd = m.toInput.Update(msg)
	}
	return m, cmd
}

// -- View ---------------------------------------------------------------------

func (m ReportsModel) View() string {
	header := m.renderHeader()

	// Filter mode fully replaces the body (matches TimersModel.viewList's
	// identical branch) rather than showing the filter form above the
	// table: the table is about to change anyway, so leaving the old rows
	// visible underneath the form while editing the range just reads as
	// stale clutter. Consistent with Timers keeps the two date-range
	// filters — which already share renderDateFilterBar — behaving
	// identically everywhere they appear.
	if m.mode == reportsModeFilter {
		return header + "\n\n" + m.renderFilterBar()
	}

	// Entries mode also fully replaces the body — see renderEntriesView.
	if m.mode == reportsModeEntries {
		return m.renderEntriesView()
	}

	// Editing an entry does too, sharing TimersModel's own form renderer —
	// see updateTimerFormFields' doc comment for why the form logic lives
	// as free functions rather than TimersModel methods.
	if m.mode == reportsModeEditEntry {
		return viewTimerForm(m.editForm, m.editFormErr, "Save")
	}

	var sb strings.Builder
	sb.WriteString(header)
	sb.WriteString("\n\n")

	if len(m.rows) == 0 {
		sb.WriteString(StyleMuted.Render("No data for the selected period."))
		return sb.String()
	}

	sb.WriteString(m.renderTable())
	sb.WriteString("\n")
	sb.WriteString(m.renderTotals())

	return sb.String()
}

func (m ReportsModel) renderHeader() string {
	var rangeStr string
	switch {
	case m.dateFrom == "" && m.dateTo == "":
		rangeStr = "All time"
	case m.dateFrom == m.dateTo && m.dateFrom != "":
		rangeStr = m.dateFrom
	default:
		from := m.dateFrom
		if from == "" {
			from = "—"
		}
		to := m.dateTo
		if to == "" {
			to = "—"
		}
		rangeStr = from + "  →  " + to
	}

	return StyleTitle.Render("Reports") +
		StyleMuted.Render("  ·  ") +
		StyleMuted.Render(rangeStr)
}

func (m ReportsModel) renderFilterBar() string {
	return renderDateFilterBar(m.fromInput, m.toInput, m.filterIdx)
}

// selectedRow returns the report row under the table cursor, or nil if
// rows is empty or the cursor is (transiently) out of bounds.
func (m ReportsModel) selectedRow() *model.ReportRow {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	return m.rows[m.cursor]
}

// selectedEntry returns the entry under entriesCursor within the currently
// selected report row, or nil if there's no selection or the cursor is
// (transiently) out of bounds.
func (m ReportsModel) selectedEntry() *model.Entry {
	r := m.selectedRow()
	if r == nil || m.entriesCursor < 0 || m.entriesCursor >= len(r.Entries) {
		return nil
	}
	return r.Entries[m.entriesCursor]
}

// entryByID searches every loaded row's entries for the one matching id.
// Used when handling reportsOpenEditMsg: the async DB round-trip to fetch
// clients/projects for the edit form leaves a gap where the user could in
// principle have moved the cursor, so this re-finds the entry by ID rather
// than trusting selectedEntry() still points at the same one.
func (m ReportsModel) entryByID(id int64) *model.Entry {
	for _, r := range m.rows {
		for _, e := range r.Entries {
			if e.ID == id {
				return e
			}
		}
	}
	return nil
}

// mainColWidths returns the CLIENT and PROJECT column widths so the report
// table spans the full available content width. The numeric columns stay fixed
// (each includes its cell padding).
func (m ReportsModel) mainColWidths() (clientW, projectW int) {
	avail := usableWidth(m.width)
	rest := avail - rColGutter - rColHours - rColEntries - rColInv - rColPaid - rColEarned
	if rest < 0 {
		rest = 0
	}
	clientW = rest / 2
	projectW = rest - clientW
	return clientW, projectW
}

// rowsBudget returns how many report-table rows fit given the current view
// height, after reserving room for the fixed chrome (header + blank + table
// header + divider) and the fixed 5-line totals section (divider + totals
// row + blank + 2-line summary cards — see renderTotals). Doesn't need to
// reserve room for a row's entries any more — those show in their own full
// screen now (reportsModeEntries / renderEntriesView), not inline here.
func (m ReportsModel) rowsBudget() int {
	// m.height is the column height including its Padding(1, 3), so the
	// usable content area is m.height - 2. Fixed chrome: header + blank +
	// table header + divider = 4.
	budget := m.height - 2 - 4 - 5
	if budget < 1 {
		budget = 1
	}
	return budget
}

// clampOffset keeps the cursor's row inside the visible [offset,
// offset+budget) window, scrolling only when it reaches the edge — mirrors
// TimersModel.clampOffset (this table has no day-grouping, so it's the
// simpler flat-list version).
func (m *ReportsModel) clampOffset() {
	budget := m.rowsBudget()
	n := len(m.rows)
	if n <= budget {
		m.offset = 0
		return
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.offset > n-budget {
		m.offset = n - budget
	}
	if m.cursor >= m.offset+budget {
		m.offset = m.cursor - budget + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m ReportsModel) renderTable() string {
	clientW, projectW := m.mainColWidths()
	totalW := usableWidth(m.width)

	hdr := RowPrefix(false) +
		StyleTableHeader.Width(clientW).Render(truncate("Client", clientW-2)) +
		StyleTableHeader.Width(projectW).Render(truncate("Project", projectW-2)) +
		StyleTableHeader.Width(rColHours).Align(lipgloss.Right).Render("Hours") +
		StyleTableHeader.Width(rColEntries).Align(lipgloss.Right).Render("Entries") +
		StyleTableHeader.Width(rColInv).Align(lipgloss.Right).Render("Invoiced") +
		StyleTableHeader.Width(rColPaid).Align(lipgloss.Right).Render("Paid") +
		StyleTableHeader.Width(rColEarned).Align(lipgloss.Right).Render("Earned")

	m.clampOffset()
	budget := m.rowsBudget()
	start := m.offset
	if start < 0 {
		start = 0
	}
	end := start + budget
	if end > len(m.rows) {
		end = len(m.rows)
	}
	visible := m.rows[start:end]

	// divider doubles as a scroll indicator (▲ = more above, ▼ = more
	// below), matching TimersModel.viewList's identical convention.
	runes := []rune(strings.Repeat("─", totalW))
	if start > 0 {
		runes[0] = '▲'
	}
	if end < len(m.rows) {
		runes[len(runes)-1] = '▼'
	}
	divider := StyleTableDiv.Render(string(runes))

	var rows []string
	rows = append(rows, hdr, divider)
	for i, r := range visible {
		idx := start + i
		rows = append(rows, m.renderRow(r, idx == m.cursor))
	}

	return strings.Join(rows, "\n")
}

func (m ReportsModel) renderRow(r *model.ReportRow, selected bool) string {
	prefix := RowPrefix(selected)
	clientW, projectW := m.mainColWidths()

	hoursStr := fmt.Sprintf("%.2fh", r.TotalHours)
	entriesStr := fmt.Sprintf("%d", r.EntryCount)
	invStr := fmt.Sprintf("%d/%d", r.Invoiced, r.EntryCount)
	paidStr := fmt.Sprintf("%d/%d", r.Paid, r.EntryCount)
	earnedStr := fmt.Sprintf("$%.2f", r.Earnings)

	var base lipgloss.Style
	if selected {
		base = StyleTableRowSel
	} else {
		base = StyleTableRow
	}

	// colour-coded sub-columns only when not selected
	earnedStyle := base
	invStyle := base
	paidStyle := base
	hoursStyle := base

	if !selected {
		// Based on StyleTableRow (not a bare lipgloss.NewStyle()) so these
		// color-coded cells keep the same Padding(0, 1) as the header and the
		// selected-row style — otherwise their text starts one column left of
		// where the column header sits.
		hoursStyle = StyleTableRow.Foreground(cHighlight)
		if r.Earnings > 0 {
			earnedStyle = StyleTableRow.Foreground(cGreen)
		} else {
			earnedStyle = StyleTableRow.Foreground(cDim)
		}
		switch {
		case r.Invoiced == r.EntryCount && r.EntryCount > 0:
			invStyle = StyleTableRow.Foreground(cAccent)
		case r.Invoiced > 0:
			invStyle = StyleTableRow.Foreground(cAmber)
		default:
			invStyle = StyleTableRow.Foreground(cDim)
		}
		switch {
		case r.Paid == r.EntryCount && r.EntryCount > 0:
			paidStyle = StyleTableRow.Foreground(cGreen)
		case r.Paid > 0:
			paidStyle = StyleTableRow.Foreground(cAmber)
		default:
			paidStyle = StyleTableRow.Foreground(cDim)
		}
	}

	return prefix +
		base.Width(clientW).Render(truncate(r.ClientName, clientW-3)) +
		base.Width(projectW).Render(truncate(r.ProjectName, projectW-3)) +
		hoursStyle.Width(rColHours).Align(lipgloss.Right).Render(hoursStr) +
		base.Width(rColEntries).Align(lipgloss.Right).Render(entriesStr) +
		invStyle.Width(rColInv).Align(lipgloss.Right).Render(invStr) +
		paidStyle.Width(rColPaid).Align(lipgloss.Right).Render(paidStr) +
		earnedStyle.Width(rColEarned).Align(lipgloss.Right).Render(earnedStr)
}

func (m ReportsModel) renderTotals() string {
	var totalHours, totalEarnings float64
	var totalEntries, totalInvoiced, totalPaid int
	for _, r := range m.rows {
		totalHours += r.TotalHours
		totalEarnings += r.Earnings
		totalEntries += r.EntryCount
		totalInvoiced += r.Invoiced
		totalPaid += r.Paid
	}

	clientW, projectW := m.mainColWidths()
	totalW := usableWidth(m.width)
	divider := StyleTableDiv.Render(strings.Repeat("─", totalW))

	totals := RowPrefix(false) +
		StyleTableHeader.Width(clientW+projectW).Render(truncate("TOTALS", clientW+projectW-2)) +
		StyleTableHeader.Copy().Foreground(cHighlight).Width(rColHours).Align(lipgloss.Right).Render(fmt.Sprintf("%.2fh", totalHours)) +
		StyleTableHeader.Width(rColEntries).Align(lipgloss.Right).Render(fmt.Sprintf("%d", totalEntries)) +
		StyleTableHeader.Width(rColInv).Align(lipgloss.Right).Render(fmt.Sprintf("%d/%d", totalInvoiced, totalEntries)) +
		StyleTableHeader.Width(rColPaid).Align(lipgloss.Right).Render(fmt.Sprintf("%d/%d", totalPaid, totalEntries)) +
		StyleTableHeader.Copy().Foreground(cGreen).Width(rColEarned).Align(lipgloss.Right).Render(fmt.Sprintf("$%.2f", totalEarnings))

	cards := m.renderSummaryCards(totalHours, totalEarnings, totalEntries, totalInvoiced, totalPaid)

	// Plain "\n" join, not lipgloss.JoinVertical — see renderConfirmDelete in
	// common.go for why: JoinVertical would pad the narrower `cards` line(s) to
	// the divider's width using unstyled spaces, punching a hole to their right.
	return divider + "\n" + totals + "\n\n" + cards
}

func (m ReportsModel) renderSummaryCards(hours, earned float64, entries, invoiced, paid int) string {
	outstanding := earned
	for _, r := range m.rows {
		if r.Paid == r.EntryCount && r.EntryCount > 0 {
			outstanding -= r.Earnings
		}
	}
	unpaidEntries := entries - paid

	card := func(label, value string, vs lipgloss.Style, withBorder bool) string {
		// The box style itself carries Background(cBg): lipgloss's own Padding
		// fill (and the fill it adds to left-align the shorter of label/value)
		// respects the style's background, unlike lipgloss.JoinVertical's raw
		// unstyled padding — see renderConfirmDelete in common.go. Margin is a
		// *separate* property in lipgloss (MarginBackground, distinct from
		// Background) and defaults to no color at all, so MarginRight needs its
		// own explicit background or the gap between cards shows the terminal's
		// default background instead of cBg.
		return lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, withBorder).
			BorderForeground(cBorder).
			BorderBackground(cBg).
			Background(cBg).
			PaddingLeft(2).
			MarginRight(4).
			MarginBackground(cBg).
			Render(StyleMuted.Render(label) + "\n" + vs.Bold(true).Background(cBg).Render(value))
	}

	uninvStyle := StyleDanger
	if unpaidEntries == 0 {
		uninvStyle = StyleSuccess
	}

	return lipgloss.JoinHorizontal(lipgloss.Top,
		// No left border on the first card — there's nothing to its left to
		// separate it from, so the bar just looked like a stray leading rule.
		card("Total Hours", fmt.Sprintf("%.2f h", hours), lipgloss.NewStyle().Foreground(cHighlight), false),
		card("Total Earned", fmt.Sprintf("$%.2f", earned), StyleSuccess, true),
		card("Outstanding", fmt.Sprintf("$%.2f", outstanding), StyleWarning, true),
		card("Uninvoiced", fmt.Sprintf("%d entries", unpaidEntries), uninvStyle, true),
	)
}

// entryCols returns the column widths for both renderEntriesView's table and
// its scroll-budget math, so the two can't drift apart. TASK expands to fill
// whatever's left.
func (m ReportsModel) entryCols() (date, task, dur, earned, status int) {
	date, dur, earned, status = 14, 12, 12, 12
	// 2 = RowPrefix's gutter width. Regression note: this used to subtract
	// a hardcoded 8 here (a stale guess at the status column's width, from
	// before it was widened to 12 to match the other fixed columns) instead
	// of the actual `status` value — every row rendered 4 columns wider
	// than totalW as a result. Since app.go's outer column wraps the whole
	// view in Width(cw), and lipgloss word-wraps (never truncates) content
	// wider than that, every entry row silently wrapped onto an extra
	// line — doubling the effective table height and overflowing well past
	// entriesRowsBudget, which pushed the header (and everything above the
	// overflow) off the top of the screen.
	task = usableWidth(m.width) - (2 + date + dur + earned + status)
	if task < 20 {
		task = 20
	}
	return date, task, dur, earned, status
}

// renderEntryDetail shows the currently selected entry's Started/End time,
// Notes (if any), and billing status — mirrors TimersModel.renderDetail
// (including reusing invoiceLabel) so the two views' detail panes look and
// behave identically.
func (m ReportsModel) renderEntryDetail(e *model.Entry) string {
	started := e.StartTime.Local().Format("Jan 2, 2006  3:04 PM")
	dateLine := StyleMuted.Render("Started  ") + StyleTableRow.Render(started)
	if e.EndTime != nil {
		dateLine += StyleMuted.Render("  –  ") + StyleTableRow.Render(e.EndTime.Local().Format("3:04 PM"))
	}

	notesLine := StyleMuted.Render(noteDetailLabel) + formatNoteDetail(e.Notes, usableWidth(m.width))
	statusLine := StyleMuted.Render("Status   ") + invoiceLabel(e)
	return strings.Join([]string{dateLine, notesLine, statusLine}, "\n")
}

// entriesRowsBudget returns how many entry rows fit in the full entries
// view: header + blank + table header + divider + blank before the detail
// pane = 5 fixed lines, plus the detail pane's own fixed height (see
// entryDetailLines — mirrors TimersModel.rowsBudget's identical dependency
// on its own detail pane).
func (m ReportsModel) entriesRowsBudget() int {
	detailLines := 1
	if m.selectedEntry() != nil {
		detailLines = entryDetailLines
	}
	budget := m.height - 2 - 5 - detailLines
	if budget < 1 {
		budget = 1
	}
	return budget
}

// clampEntriesOffset keeps entriesCursor's row inside the visible [offset,
// offset+budget) window of the selected row's entries, scrolling only when
// it reaches the edge — mirrors TimersModel.clampOffset (this list has no
// day-grouping, so it's the simpler flat-list version).
func (m *ReportsModel) clampEntriesOffset() {
	r := m.selectedRow()
	if r == nil {
		m.entriesOffset = 0
		return
	}
	n := len(r.Entries)
	budget := m.entriesRowsBudget()
	if n <= budget {
		m.entriesOffset = 0
		return
	}
	if m.entriesCursor < m.entriesOffset {
		m.entriesOffset = m.entriesCursor
	}
	if m.entriesOffset > n-budget {
		m.entriesOffset = n - budget
	}
	if m.entriesCursor >= m.entriesOffset+budget {
		m.entriesOffset = m.entriesCursor - budget + 1
	}
	if m.entriesOffset < 0 {
		m.entriesOffset = 0
	}
}

// renderEntriesView shows the full, scrollable list of entries for the
// currently selected report row — its own dedicated screen (see
// reportsModeEntries) rather than a capped inline preview competing with
// the report table for space, so a client/project with many logged
// sessions is fully browsable, not truncated with a "… and N more" note.
// The selected entry's Started/End/Notes/Status show in a detail pane
// below the table, mirroring TimersModel.viewList's identical layout.
func (m ReportsModel) renderEntriesView() string {
	r := m.selectedRow()
	if r == nil {
		return m.renderHeader()
	}

	header := StyleTitle.Render(r.ClientName+" › "+r.ProjectName) +
		StyleMuted.Render("  ·  ") +
		StyleMuted.Render(fmt.Sprintf("%.2fh billed  ·  $%.2f", r.TotalHours, r.Earnings))

	if len(r.Entries) == 0 {
		return header + "\n\n" + StyleMuted.Render("No entries for this period.")
	}

	eColDate, eColTask, eColDur, eColEarned, eColStatus := m.entryCols()
	totalW := usableWidth(m.width)

	hdr := RowPrefix(false) +
		StyleTableHeader.Width(eColDate).Render("Date") +
		StyleTableHeader.Width(eColTask).Render("Task") +
		StyleTableHeader.Width(eColDur).Align(lipgloss.Right).Render("Duration") +
		StyleTableHeader.Width(eColEarned).Align(lipgloss.Right).Render("Earned") +
		StyleTableHeader.Width(eColStatus).Render("Status")

	m.clampEntriesOffset()
	budget := m.entriesRowsBudget()
	start := m.entriesOffset
	if start < 0 {
		start = 0
	}
	end := start + budget
	if end > len(r.Entries) {
		end = len(r.Entries)
	}
	visible := r.Entries[start:end]

	// divider doubles as a scroll indicator (▲ = more above, ▼ = more
	// below), matching renderTable's identical convention.
	runes := []rune(strings.Repeat("─", totalW))
	if start > 0 {
		runes[0] = '▲'
	}
	if end < len(r.Entries) {
		runes[len(runes)-1] = '▼'
	}
	divider := StyleTableDiv.Render(string(runes))

	rows := []string{hdr, divider}
	for i, e := range visible {
		idx := start + i
		sel := idx == m.entriesCursor

		base := StyleTableRow
		if sel {
			base = StyleTableRowSel
		}

		date := e.StartTime.Local().Format("Jan 2, 2006")
		task := truncate(e.Task, eColTask-3)
		dur := model.FormatDuration(e.Duration())

		var earnedStr string
		if r.HourlyRate > 0 {
			earnedStr = fmt.Sprintf("$%.2f", e.Earnings(r.HourlyRate))
		} else {
			earnedStr = "—"
		}

		// rowBg mirrors `base`'s own background (cBgAlt when selected, cBg
		// otherwise) so the status cell below — which needs its own fixed
		// foreground independent of `base` — still completes the selected
		// row's tint instead of leaving a plain-cBg gap in it.
		rowBg := cBg
		if sel {
			rowBg = cBgAlt
		}
		var statusStr string
		switch {
		case e.Paid:
			statusStr = StyleSuccess.Background(rowBg).Width(eColStatus).Render("paid")
		case e.Invoiced:
			statusStr = StyleAccent.Background(rowBg).Width(eColStatus).Render("invoiced")
		default:
			statusStr = StyleMuted.Background(rowBg).Width(eColStatus).Render("pending")
		}

		row := RowPrefix(sel) +
			base.Width(eColDate).Render(date) +
			base.Width(eColTask).Render(task) +
			base.Width(eColDur).Align(lipgloss.Right).Render(dur) +
			base.Width(eColEarned).Align(lipgloss.Right).Render(earnedStr) +
			statusStr

		rows = append(rows, row)
	}

	tableStr := strings.Join(rows, "\n")
	detail := ""
	if e := m.selectedEntry(); e != nil {
		detail = m.renderEntryDetail(e)
	}

	return header + "\n\n" + tableStr + "\n\n" + detail
}
