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

// ── Table columns ─────────────────────────────────────────────────────────────
//
// Mirrors the fixed-column + flexible-column layout used by the other three
// views (see ProjectsModel.listColWidths, ReportsModel.mainColWidths):
// short columns get a fixed width, TASK/PROJECT split whatever is left.

const (
	tColGutter = 2
	// tColStatus must fit the widest badge renderBadge produces: Padding(0, 1)
	// on each side of "INVOICED" (8 chars) = 10. Narrower than that and the
	// row overflows past its intended width, which wraps EARNED onto the
	// next line instead of truncating.
	tColStatus   = 12
	tColDuration = 11
	tColEarned   = 12
)

// listColWidths returns the TASK and PROJECT column widths so the table
// spans the full available content width. PROJECT gets an even split (not a
// smaller share): its cell renders "Client › Project" concatenated, which
// tends to run longer than a single TASK description, so a Task-heavy split
// left PROJECT truncating far more often than TASK did.
func (m TimersModel) listColWidths() (taskCol, projectCol int) {
	avail := usableWidth(m.width) - tColGutter - tColStatus - tColDuration - tColEarned
	projectCol = avail / 2
	if projectCol < 18 {
		projectCol = 18
	}
	if projectCol > 44 {
		projectCol = 44
	}
	taskCol = avail - projectCol
	if taskCol < 18 {
		taskCol = 18
	}
	return taskCol, projectCol
}

// ── Modes ─────────────────────────────────────────────────────────────────────

type timersMode int

const (
	timersModeList timersMode = iota
	timersModeNew
	timersModeEdit
	timersModeConfirmDelete
	timersModeFilter
)

// ── Form field indices ────────────────────────────────────────────────────────

const (
	fieldTask    = 0
	fieldClient  = 1
	fieldProject = 2
	fieldStart   = 3 // edit mode only — see timerForm.entryID
	fieldEnd     = 4 // edit mode only — see timerForm.entryID
	fieldNotes   = 5
	fieldSave    = 6
	fieldCancel  = 7
	fieldCount   = 8
)

// ── Key bindings ──────────────────────────────────────────────────────────────

type timersKeyMap struct {
	Up        key.Binding
	Down      key.Binding
	New       key.Binding
	Edit      key.Binding
	Delete    key.Binding
	Toggle    key.Binding
	Invoice   key.Binding
	Paid      key.Binding
	Today     key.Binding
	Yesterday key.Binding
	Week      key.Binding
	All       key.Binding
	Filter    key.Binding
	Confirm   key.Binding
	Cancel    key.Binding
}

var timersKeys = timersKeyMap{
	Up:        key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:      key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	New:       key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
	Edit:      key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
	Delete:    key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
	Toggle:    key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "start/stop")),
	Invoice:   key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "toggle invoice")),
	Paid:      key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "toggle paid")),
	Today:     key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "today")),
	Yesterday: key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "yesterday")),
	Week:      key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "this week")),
	All:       key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "all time")),
	Filter:    key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "custom range")),
	Confirm:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
	Cancel:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
}

// ── Dropdown item ─────────────────────────────────────────────────────────────

type dropdownItem struct {
	id    int64 // 0 = "(create new)"
	label string
}

// ── Form ──────────────────────────────────────────────────────────────────────

// timerForm holds all state for the new/edit timer form.
// Client and project fields support live fuzzy filtering and inline creation.
type timerForm struct {
	entryID int64 // 0 = new

	inputs   [fieldCount]textinput.Model
	focusIdx int

	// client dropdown
	allClients    []*model.Client
	clientMatches []dropdownItem
	clientSel     int   // index into clientMatches
	clientID      int64 // 0 = will be created from input text

	// project dropdown
	allProjects    []*model.Project
	projectMatches []dropdownItem
	projectSel     int
	projectID      int64 // 0 = will be created from input text

	showClientDrop  bool
	showProjectDrop bool
}

func newTimerForm(clients []*model.Client, projects []*model.Project) timerForm {
	var inputs [fieldCount]textinput.Model
	inputs[fieldTask] = makeTextInput("What are you working on?", 200, 44)
	inputs[fieldClient] = makeTextInput("Client name (or leave blank)", 64, 44)
	inputs[fieldProject] = makeTextInput("Project name (or leave blank)", 64, 44)
	// Width 44 (not something tighter around CharLimit's 16) to match every
	// other field in this form: ti.Width caps the placeholder-rendering
	// budget independently of the outer 48-wide box, so a narrower value
	// here doesn't gain anything (CharLimit already caps typed length) and
	// was truncating End's 34-character placeholder even though the box
	// had plenty of room.
	inputs[fieldStart] = makeTextInput("YYYY-MM-DD HH:MM", 16, 44)
	inputs[fieldEnd] = makeTextInput("YYYY-MM-DD HH:MM (blank = running)", 16, 44)
	inputs[fieldNotes] = makeTextInput("Notes (optional)", 500, 44)
	inputs[fieldTask].Focus()

	f := timerForm{
		inputs:      inputs,
		focusIdx:    fieldTask,
		allClients:  clients,
		allProjects: projects,
	}
	f.rebuildClientMatches()
	f.rebuildProjectMatches()
	return f
}

// rebuildClientMatches filters allClients by the current client input text.
func (f *timerForm) rebuildClientMatches() {
	query := strings.ToLower(strings.TrimSpace(f.inputs[fieldClient].Value()))
	f.clientMatches = nil

	for _, c := range f.allClients {
		if query == "" || strings.Contains(strings.ToLower(c.Name), query) {
			f.clientMatches = append(f.clientMatches, dropdownItem{id: c.ID, label: c.Name})
		}
	}

	// Always offer "create new" if typed text doesn't exactly match an existing name
	exactMatch := false
	for _, c := range f.allClients {
		if strings.EqualFold(c.Name, query) {
			exactMatch = true
			break
		}
	}
	if query != "" && !exactMatch {
		f.clientMatches = append(f.clientMatches, dropdownItem{id: 0, label: fmt.Sprintf(`+ Create "%s"`, f.inputs[fieldClient].Value())})
	}

	// clamp selection
	if f.clientSel >= len(f.clientMatches) {
		f.clientSel = 0
	}
}

// rebuildProjectMatches filters allProjects by current text, scoped to selected client.
func (f *timerForm) rebuildProjectMatches() {
	query := strings.ToLower(strings.TrimSpace(f.inputs[fieldProject].Value()))
	f.projectMatches = nil

	for _, p := range f.allProjects {
		// If a client is selected, only show that client's projects
		if f.clientID != 0 && p.ClientID != f.clientID {
			continue
		}
		if query == "" || strings.Contains(strings.ToLower(p.Name), query) {
			label := p.Name
			if f.clientID == 0 && p.Client != nil {
				label = fmt.Sprintf("%s › %s", p.Client.Name, p.Name)
			}
			f.projectMatches = append(f.projectMatches, dropdownItem{id: p.ID, label: label})
		}
	}

	// Offer "create new" if typed text doesn't exactly match
	exactMatch := false
	for _, p := range f.allProjects {
		if strings.EqualFold(p.Name, query) {
			exactMatch = true
			break
		}
	}
	if query != "" && !exactMatch {
		f.projectMatches = append(f.projectMatches, dropdownItem{id: 0, label: fmt.Sprintf(`+ Create "%s"`, f.inputs[fieldProject].Value())})
	}

	if f.projectSel >= len(f.projectMatches) {
		f.projectSel = 0
	}
}

// applyClientSelection commits the highlighted dropdown item to the client field.
func (f *timerForm) applyClientSelection() {
	if len(f.clientMatches) == 0 {
		return
	}
	item := f.clientMatches[f.clientSel]
	if item.id == 0 {
		// "create new" — keep the typed text, clear ID
		f.clientID = 0
		f.clearProject()
	} else if item.id != f.clientID {
		f.clearProject()
		f.clientID = item.id
		// Find the name and set it
		for _, c := range f.allClients {
			if c.ID == item.id {
				f.inputs[fieldClient].SetValue(c.Name)
				break
			}
		}
	}
	f.showClientDrop = false
	f.rebuildProjectMatches()
}

// clearProject resets the project field so a stale selection isn't kept when the client changes.
func (f *timerForm) clearProject() {
	f.inputs[fieldProject].SetValue("")
	f.projectID = 0
	f.projectSel = 0
}

// applyProjectSelection commits the highlighted dropdown item to the project field.
func (f *timerForm) applyProjectSelection() {
	if len(f.projectMatches) == 0 {
		return
	}
	item := f.projectMatches[f.projectSel]
	if item.id == 0 {
		f.projectID = 0
	} else {
		f.projectID = item.id
		for _, p := range f.allProjects {
			if p.ID == item.id {
				f.inputs[fieldProject].SetValue(p.Name)
				// also fill client if blank
				if f.clientID == 0 && p.Client != nil {
					f.clientID = p.Client.ID
					f.inputs[fieldClient].SetValue(p.Client.Name)
				}
				break
			}
		}
	}
	f.showProjectDrop = false
}

// fieldAfterProject returns the field that follows Project in tab order:
// Start (so the start/end time can be corrected) when editing an existing
// entry, or straight to Notes for a brand-new timer, which always starts
// "now" and has no times to edit yet.
func (f *timerForm) fieldAfterProject() int {
	if f.entryID != 0 {
		return fieldStart
	}
	return fieldNotes
}

// fieldBeforeNotes is fieldAfterProject's inverse, for Notes' shift+tab.
func (f *timerForm) fieldBeforeNotes() int {
	if f.entryID != 0 {
		return fieldEnd
	}
	return fieldProject
}

// focusField blurs all inputs then focuses the given one.
func (f *timerForm) focusField(idx int) {
	for i := range f.inputs {
		f.inputs[i].Blur()
	}
	f.focusIdx = idx
	if idx < fieldSave {
		f.inputs[idx].Focus()
	}
	f.showClientDrop = idx == fieldClient && len(f.clientMatches) > 0
	f.showProjectDrop = idx == fieldProject && len(f.projectMatches) > 0
}

// ── Model ─────────────────────────────────────────────────────────────────────

type TimersModel struct {
	db       *db.DB
	width    int
	height   int
	mode     timersMode
	entries  []*model.Entry // all entries, as loaded from the DB
	filtered []*model.Entry // entries within [dateFrom, dateTo]; see recomputeFiltered
	cursor   int            // index into filtered
	offset   int            // index into buildLines() output; see clampOffset
	form     timerForm
	err      string

	// Date-range filter. Both are "YYYY-MM-DD" local calendar dates; "" means
	// unbounded on that side (dateFrom == dateTo == "" is "all time").
	dateFrom string
	dateTo   string
	// preset names which quick-filter produced the current dateFrom/dateTo, so
	// the header can say "This Week" or "All time" outright instead of
	// deriving a label from date math — which, whenever the data itself only
	// spans a single day (e.g. a fresh install with only today's entries),
	// would show the same "Today" for every preset and make it look like
	// switching filters did nothing.
	preset timersPreset

	fromInput textinput.Model
	toInput   textinput.Model
	filterIdx int
}

type timersPreset int

const (
	timersPresetToday timersPreset = iota
	timersPresetYesterday
	timersPresetWeek
	timersPresetAll
	timersPresetCustom
)

func NewTimers(database *db.DB) TimersModel {
	today := time.Now().Local().Format("2006-01-02")

	// dateFilterInputWidth, not renderDateFilterBar's box width directly —
	// see its doc comment in common.go for why they're 1 apart.
	from := makeTextInput("YYYY-MM-DD", 10, dateFilterInputWidth)
	from.SetValue(today)
	to := makeTextInput("YYYY-MM-DD", 10, dateFilterInputWidth)
	to.SetValue(today)

	m := TimersModel{
		db:        database,
		dateFrom:  today,
		dateTo:    today,
		preset:    timersPresetToday,
		fromInput: from,
		toInput:   to,
	}
	return m
}

func (m *TimersModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// selectedEntry returns the entry under the cursor, or nil if filtered is
// empty or the cursor is (transiently) out of bounds.
func (m TimersModel) selectedEntry() *model.Entry {
	if m.cursor < 0 || m.cursor >= len(m.filtered) {
		return nil
	}
	return m.filtered[m.cursor]
}

// recomputeFiltered rebuilds `filtered` from `entries` using the current
// [dateFrom, dateTo] range (both "YYYY-MM-DD" local dates; "" = unbounded on
// that side), and clamps the cursor to stay in range. Entries are compared by
// their LOCAL start date (not UTC — a timer started late at night is still
// "today" to the user even if start_time's UTC date has already rolled over).
// entries is already sorted DESC by start_time (from the DB query), and
// filtering preserves that order.
func (m *TimersModel) recomputeFiltered() {
	m.filtered = m.filtered[:0]
	for _, e := range m.entries {
		d := e.StartTime.Local().Format("2006-01-02")
		if m.dateFrom != "" && d < m.dateFrom {
			continue
		}
		if m.dateTo != "" && d > m.dateTo {
			continue
		}
		m.filtered = append(m.filtered, e)
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// ── Day grouping ──────────────────────────────────────────────────────────────
//
// The list is rendered as a sequence of "lines": a day-header line (just the
// day label — see renderDayHeaderLine) followed by that day's entry rows.
// Since `filtered` is sorted DESC by start time, a day's entries are always
// contiguous, so this needs only a single pass. There's no per-day total any
// more: on a multi-day filter (e.g. "This Week") repeating a Total line under
// every day read as cluttered, so there's one grand total in the table's
// footer instead (see filteredTotals/renderFooterTotals).

type timersLineKind int

const (
	timersLineHeader timersLineKind = iota
	timersLineEntry
)

type timersLine struct {
	kind     timersLineKind
	entryIdx int       // valid when kind == timersLineEntry: index into m.filtered
	day      time.Time // valid when kind == timersLineHeader
}

func (m TimersModel) buildLines() []timersLine {
	var lines []timersLine
	var curDay string
	for i, e := range m.filtered {
		day := e.StartTime.Local().Format("2006-01-02")
		if i == 0 || day != curDay {
			lines = append(lines, timersLine{kind: timersLineHeader, day: e.StartTime.Local()})
			curDay = day
		}
		lines = append(lines, timersLine{kind: timersLineEntry, entryIdx: i})
	}
	return lines
}

// filteredTotals sums duration/earnings across every currently filtered
// entry, regardless of day — the grand total shown in the table's footer
// row (see renderFooterTotals). The currently-running entry, if any, is
// included: its Duration()/Earnings() are already live (see
// model.Entry.Duration), so the total ticks up in step with that entry's own
// row above it rather than appearing "frozen" while its row keeps counting.
func (m TimersModel) filteredTotals() (time.Duration, float64) {
	var dur time.Duration
	var earned float64
	for _, e := range m.filtered {
		dur += e.Duration()
		if e.Project != nil && e.Project.Client != nil {
			earned += e.Earnings(e.Project.Client.HourlyRate)
		}
	}
	return dur, earned
}

// friendlyDay formats a local calendar day as "Today", "Yesterday", or
// "Mon, Jan 2".
func friendlyDay(t time.Time) string {
	now := time.Now().Local()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	switch {
	case day.Equal(today):
		return "Today"
	case day.Equal(today.AddDate(0, 0, -1)):
		return "Yesterday"
	default:
		return t.Format("Mon, Jan 2")
	}
}

// parseLocalDate parses a "YYYY-MM-DD" string in the local zone; ok is false
// for an empty or unparseable string.
func parseLocalDate(s string) (t time.Time, ok bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02", s, time.Local)
	return t, err == nil
}

// parseLocalDateTime parses a "YYYY-MM-DD HH:MM" string in the local zone;
// ok is false for an empty or unparseable string.
func parseLocalDateTime(s string) (t time.Time, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02 15:04", s, time.Local)
	return t, err == nil
}

// renderDetailPane returns the content below the table: the selected entry's
// detail. Delete confirmation is a floating modal (see viewList) rather than
// a swap of this pane, so it doesn't need a case here. Used by both
// rowsBudget (to size the scroll window) and viewList (to render it), so the
// two stay in sync by construction.
func (m TimersModel) renderDetailPane() string {
	if e := m.selectedEntry(); e != nil {
		return m.renderDetail(e)
	}
	return ""
}

// rowsBudget returns how many render lines (day headers + entry rows,
// including a synthesized sticky day header when one is needed — see
// viewList) fit between the header and the detail pane given the current
// view height. It's a cap on the total windowed items, not a per-entry
// count: when a sticky header is synthesized, it takes one of these slots
// rather than being added on top, so the table's total rendered height
// stays constant whether or not the window happens to start mid-day.
func (m TimersModel) rowsBudget() int {
	detailLines := 1
	if m.selectedEntry() != nil {
		detailLines = entryDetailLines
	}
	// fixed chrome: header + blank + table header + divider + footer divider
	// + footer totals row + blank = 7 lines. m.height is the column height
	// including its Padding(1, 3), so the usable content area is
	// m.height - 2.
	budget := m.height - 2 - 7 - detailLines
	if budget < 1 {
		budget = 1
	}
	return budget
}

// windowEnd returns the end index (exclusive) into lines for the window
// starting at offset with the given budget. When offset doesn't already
// land on a day header, viewList synthesizes a sticky copy of it (see the
// "Sticky day header" comment there), which consumes one of the budgeted
// slots — so the window holds one fewer real line in that case. Shared by
// clampOffset (to know whether the cursor's line will actually be visible)
// and viewList (to slice the same window it describes), so the two can't
// drift apart the way rowsBudget's old fixed reservation did.
func windowEnd(lines []timersLine, offset, budget int) int {
	entryBudget := budget
	if offset >= 0 && offset < len(lines) && lines[offset].kind != timersLineHeader {
		entryBudget--
	}
	if entryBudget < 0 {
		entryBudget = 0
	}
	end := offset + entryBudget
	if end > len(lines) {
		end = len(lines)
	}
	return end
}

// clampOffset keeps the cursor's line inside the visible window, scrolling
// only when it reaches the edge of the window. Unlike the entry-index-based
// version this replaces, offset/budget are measured in *render lines*
// (day headers count as lines too), since the number of lines no longer
// equals len(filtered).
func (m *TimersModel) clampOffset() {
	lines := m.buildLines()
	budget := m.rowsBudget()
	n := len(lines)
	if n == 0 {
		m.offset = 0
		return
	}
	if n <= budget {
		m.offset = 0
		return
	}
	cursorLine := 0
	for i, l := range lines {
		if l.kind == timersLineEntry && l.entryIdx == m.cursor {
			cursorLine = i
			break
		}
	}
	if cursorLine < m.offset {
		m.offset = cursorLine
	}
	if m.offset > n-budget {
		m.offset = n - budget
	}
	// Scrolling down: keep the cursor within the window that will actually
	// render, which may hold one fewer line than budget when a sticky day
	// header is synthesized at this offset (see windowEnd).
	for cursorLine >= windowEnd(lines, m.offset, budget) {
		m.offset++
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m TimersModel) IsBusy() bool {
	return m.mode != timersModeList
}

// Help returns the contextual hotkey string for the view's current mode, for
// display in the app-level hotkey bar (see app.go renderHotkeyBar).
func (m TimersModel) Help() []Hotkey {
	switch m.mode {
	case timersModeNew, timersModeEdit:
		return []Hotkey{
			{"tab / shift+tab", "move"},
			{"↑ / ↓", "dropdown"},
			{"enter", "confirm"},
			{"esc", "cancel"},
		}
	case timersModeConfirmDelete:
		return []Hotkey{{"y", "confirm"}, {"esc", "cancel"}}
	case timersModeFilter:
		return []Hotkey{{"tab", "switch"}, {"enter", "apply"}, {"esc", "cancel"}}
	default:
		return []Hotkey{
			{"n", "new"},
			{"e", "edit"},
			{"d", "delete"},
			{"space", "toggle"},
			{"t", "today"},
			{"y", "yesterday"},
			{"w", "week"},
			{"a", "all"},
			{"f", "range"},
		}
	}
}

func (m TimersModel) Init() tea.Cmd {
	return loadTimerEntriesCmd(m.db)
}

// ── Update ────────────────────────────────────────────────────────────────────

func (m TimersModel) Update(msg tea.Msg) (TimersModel, tea.Cmd) {
	switch msg := msg.(type) {

	case timerEntriesMsg:
		m.entries = []*model.Entry(msg)
		m.recomputeFiltered()
		m.clampOffset()
		return m, nil

	case openFormMsg:
		m = m.handleOpenForm(msg)
		return m, nil

	case tea.KeyMsg:
		switch m.mode {
		case timersModeList:
			return m.updateList(msg)
		case timersModeNew, timersModeEdit:
			return m.updateForm(msg)
		case timersModeConfirmDelete:
			return m.updateConfirm(msg)
		case timersModeFilter:
			return m.updateFilter(msg)
		}
	}
	return m, nil
}

// ── List update ───────────────────────────────────────────────────────────────

func (m TimersModel) updateList(msg tea.KeyMsg) (TimersModel, tea.Cmd) {
	switch {
	case key.Matches(msg, timersKeys.Up):
		if m.cursor > 0 {
			m.cursor--
			m.clampOffset()
		}

	case key.Matches(msg, timersKeys.Down):
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
			m.clampOffset()
		}

	case key.Matches(msg, timersKeys.New):
		return m, m.openFormCmd(0)

	case key.Matches(msg, timersKeys.Edit):
		if e := m.selectedEntry(); e != nil {
			return m, m.openFormCmd(e.ID)
		}

	case key.Matches(msg, timersKeys.Delete):
		if len(m.filtered) > 0 {
			m.mode = timersModeConfirmDelete
		}

	case key.Matches(msg, timersKeys.Toggle):
		if entry := m.selectedEntry(); entry != nil {
			if entry.IsRunning() {
				return m, m.stopCmd(entry.ID)
			}
			return m, m.startCmd(entry.ProjectID, entry.Task)
		}

	case key.Matches(msg, timersKeys.Invoice):
		if entry := m.selectedEntry(); entry != nil {
			if entry.IsRunning() {
				return m, ErrCmd("Stop the timer before invoicing")
			}
			return m, m.toggleInvoiceCmd(entry)
		}

	case key.Matches(msg, timersKeys.Paid):
		if entry := m.selectedEntry(); entry != nil {
			if !entry.Invoiced {
				return m, ErrCmd("Mark as invoiced first (press i)")
			}
			return m, m.togglePaidCmd(entry)
		}

	case key.Matches(msg, timersKeys.Today):
		today := time.Now().Local().Format("2006-01-02")
		m.dateFrom, m.dateTo = today, today
		m.preset = timersPresetToday
		m.fromInput.SetValue(today)
		m.toInput.SetValue(today)
		m.cursor = 0
		m.recomputeFiltered()

	case key.Matches(msg, timersKeys.Yesterday):
		yesterday := time.Now().Local().AddDate(0, 0, -1).Format("2006-01-02")
		m.dateFrom, m.dateTo = yesterday, yesterday
		m.preset = timersPresetYesterday
		m.fromInput.SetValue(yesterday)
		m.toInput.SetValue(yesterday)
		m.cursor = 0
		m.recomputeFiltered()

	case key.Matches(msg, timersKeys.Week):
		now := time.Now().Local()
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		monday := now.AddDate(0, 0, -(weekday - 1))
		m.dateFrom = monday.Format("2006-01-02")
		m.dateTo = now.Format("2006-01-02")
		m.preset = timersPresetWeek
		m.fromInput.SetValue(m.dateFrom)
		m.toInput.SetValue(m.dateTo)
		m.cursor = 0
		m.recomputeFiltered()

	case key.Matches(msg, timersKeys.All):
		m.dateFrom, m.dateTo = "", ""
		m.preset = timersPresetAll
		m.fromInput.SetValue("")
		m.toInput.SetValue("")
		m.cursor = 0
		m.recomputeFiltered()

	case key.Matches(msg, timersKeys.Filter):
		m.mode = timersModeFilter
		m.filterIdx = 0
		m.fromInput.Focus()
		m.toInput.Blur()
		return m, textinput.Blink
	}
	return m, nil
}

// openFormCmd loads clients+projects then opens the form.
func (m TimersModel) openFormCmd(entryID int64) tea.Cmd {
	return func() tea.Msg {
		clients, err := m.db.ListClients()
		if err != nil {
			return ErrMsg(err.Error())
		}
		projects, err := m.db.ListProjects(0)
		if err != nil {
			return ErrMsg(err.Error())
		}
		return openFormMsg{entryID: entryID, clients: clients, projects: projects}
	}
}

type openFormMsg struct {
	entryID  int64
	clients  []*model.Client
	projects []*model.Project
}

// ── Form update ───────────────────────────────────────────────────────────────

func (m TimersModel) updateForm(msg tea.KeyMsg) (TimersModel, tea.Cmd) {
	action, cmd := updateTimerFormFields(&m.form, msg)
	switch action {
	case "cancel":
		m.mode = timersModeList
		m.err = ""
		return m, nil
	case "submit":
		return m.submitForm()
	}
	return m, cmd
}

// updateTimerFormFields handles field navigation, dropdown selection, and
// text input for a timer/entry form — shared by TimersModel's own New/Edit
// Timer form and ReportsModel's "edit entry" flow (see
// ReportsModel.updateEditEntry), so the two forms behave identically and
// can't drift apart. It's a free function, not a TimersModel method,
// deliberately: app.go broadcasts every non-key tea.Msg to *all* tabs, not
// just the active one, so anything routed through TimersModel-specific
// state or message types here would also fire on the real Timers tab even
// while editing from Reports. Operating purely on a *timerForm sidesteps
// that entirely.
//
// It doesn't perform Cancel/Save itself, since those differ per caller
// (TimersModel returns to its list, ReportsModel to its entries view; only
// TimersModel's Save path can create a brand new entry) — it just signals
// the caller via the returned action: "cancel", "submit", or "" for an
// ordinary keystroke.
func updateTimerFormFields(f *timerForm, msg tea.KeyMsg) (action string, cmd tea.Cmd) {
	// Always allow cancel
	if key.Matches(msg, timersKeys.Cancel) {
		return "cancel", nil
	}

	switch f.focusIdx {

	// ── Task field ────────────────────────────────────────────────────────────
	case fieldTask:
		switch {
		case msg.Type == tea.KeyTab:
			f.focusField(fieldClient)
		case msg.Type == tea.KeyShiftTab:
			f.focusField(fieldSave)
		case key.Matches(msg, timersKeys.Confirm):
			f.focusField(fieldClient)
		default:
			f.inputs[fieldTask], cmd = f.inputs[fieldTask].Update(msg)
			return "", cmd
		}

	// ── Client field ──────────────────────────────────────────────────────────
	case fieldClient:
		switch {
		case msg.Type == tea.KeyTab:
			f.applyClientSelection()
			f.focusField(fieldProject)
		case msg.Type == tea.KeyShiftTab:
			f.applyClientSelection()
			f.focusField(fieldTask)
		case key.Matches(msg, timersKeys.Confirm):
			f.applyClientSelection()
			f.focusField(fieldProject)
		case key.Matches(msg, timersKeys.Up):
			if f.clientSel > 0 {
				f.clientSel--
			}
			f.showClientDrop = true
		case key.Matches(msg, timersKeys.Down):
			if f.clientSel < len(f.clientMatches)-1 {
				f.clientSel++
			}
			f.showClientDrop = true
		default:
			f.inputs[fieldClient], cmd = f.inputs[fieldClient].Update(msg)
			// Reset resolved ID when user edits the text
			f.clientID = 0
			f.clearProject()
			f.rebuildClientMatches()
			f.showClientDrop = len(f.clientMatches) > 0
			f.rebuildProjectMatches()
			return "", cmd
		}

	// ── Project field ─────────────────────────────────────────────────────────
	case fieldProject:
		switch {
		case msg.Type == tea.KeyTab:
			f.applyProjectSelection()
			f.focusField(f.fieldAfterProject())
		case msg.Type == tea.KeyShiftTab:
			f.applyProjectSelection()
			f.focusField(fieldClient)
		case key.Matches(msg, timersKeys.Confirm):
			f.applyProjectSelection()
			f.focusField(f.fieldAfterProject())
		case key.Matches(msg, timersKeys.Up):
			if f.projectSel > 0 {
				f.projectSel--
			}
			f.showProjectDrop = true
		case key.Matches(msg, timersKeys.Down):
			if f.projectSel < len(f.projectMatches)-1 {
				f.projectSel++
			}
			f.showProjectDrop = true
		default:
			f.inputs[fieldProject], cmd = f.inputs[fieldProject].Update(msg)
			f.projectID = 0
			f.rebuildProjectMatches()
			f.showProjectDrop = len(f.projectMatches) > 0
			return "", cmd
		}

	// ── Start field (edit mode only) ─────────────────────────────────────────
	case fieldStart:
		switch {
		case msg.Type == tea.KeyTab:
			f.focusField(fieldEnd)
		case msg.Type == tea.KeyShiftTab:
			f.focusField(fieldProject)
		case key.Matches(msg, timersKeys.Confirm):
			f.focusField(fieldEnd)
		default:
			f.inputs[fieldStart], cmd = f.inputs[fieldStart].Update(msg)
			return "", cmd
		}

	// ── End field (edit mode only) ───────────────────────────────────────────
	case fieldEnd:
		switch {
		case msg.Type == tea.KeyTab:
			f.focusField(fieldNotes)
		case msg.Type == tea.KeyShiftTab:
			f.focusField(fieldStart)
		case key.Matches(msg, timersKeys.Confirm):
			f.focusField(fieldNotes)
		default:
			f.inputs[fieldEnd], cmd = f.inputs[fieldEnd].Update(msg)
			return "", cmd
		}

	// ── Notes field ───────────────────────────────────────────────────────────
	case fieldNotes:
		switch {
		case msg.Type == tea.KeyTab:
			f.focusField(fieldSave)
		case msg.Type == tea.KeyShiftTab:
			f.focusField(f.fieldBeforeNotes())
		case key.Matches(msg, timersKeys.Confirm):
			f.focusField(fieldSave)
		default:
			f.inputs[fieldNotes], cmd = f.inputs[fieldNotes].Update(msg)
			return "", cmd
		}

	// ── Save button ───────────────────────────────────────────────────────────
	case fieldSave:
		switch {
		case msg.Type == tea.KeyTab:
			f.focusField(fieldCancel)
		case msg.Type == tea.KeyShiftTab:
			f.focusField(fieldNotes)
		case key.Matches(msg, timersKeys.Confirm):
			return "submit", nil
		}

	// ── Cancel button ─────────────────────────────────────────────────────────
	case fieldCancel:
		switch {
		case msg.Type == tea.KeyTab:
			f.focusField(fieldTask)
		case msg.Type == tea.KeyShiftTab:
			f.focusField(fieldSave)
		case key.Matches(msg, timersKeys.Confirm):
			return "cancel", nil
		}
	}

	return "", nil
}

// ── Confirm delete ────────────────────────────────────────────────────────────

func (m TimersModel) updateConfirm(msg tea.KeyMsg) (TimersModel, tea.Cmd) {
	switch {
	// Deliberately "y"/"Y" only, not the shared Confirm (enter) binding — a
	// destructive delete shouldn't fire from a reflexive Enter press. Mirrors
	// clients.go/projects.go's confirm-delete dispatch.
	case msg.String() == "y", msg.String() == "Y":
		m.mode = timersModeList
		e := m.selectedEntry()
		if e == nil {
			return m, nil
		}
		return m, m.deleteCmd(e.ID)
	case key.Matches(msg, timersKeys.Cancel):
		m.mode = timersModeList
	}
	return m, nil
}

// ── Filter update ─────────────────────────────────────────────────────────────

func (m TimersModel) updateFilter(msg tea.KeyMsg) (TimersModel, tea.Cmd) {
	switch {
	case key.Matches(msg, timersKeys.Cancel):
		m.mode = timersModeList
		m.fromInput.Blur()
		m.toInput.Blur()
		return m, nil

	case key.Matches(msg, timersKeys.Confirm):
		m.dateFrom = strings.TrimSpace(m.fromInput.Value())
		m.dateTo = strings.TrimSpace(m.toInput.Value())
		m.preset = timersPresetCustom
		m.mode = timersModeList
		m.fromInput.Blur()
		m.toInput.Blur()
		m.cursor = 0
		m.recomputeFiltered()
		return m, nil

	case msg.Type == tea.KeyTab, msg.Type == tea.KeyShiftTab:
		m.fromInput.Blur()
		m.toInput.Blur()
		if msg.Type == tea.KeyTab {
			m.filterIdx = (m.filterIdx + 1) % 2
		} else {
			m.filterIdx = (m.filterIdx - 1 + 2) % 2
		}
		if m.filterIdx == 0 {
			m.fromInput.Focus()
		} else {
			m.toInput.Focus()
		}
		return m, textinput.Blink
	}

	var cmd tea.Cmd
	if m.filterIdx == 0 {
		m.fromInput, cmd = m.fromInput.Update(msg)
	} else {
		m.toInput, cmd = m.toInput.Update(msg)
	}
	return m, cmd
}

// ── Commands ──────────────────────────────────────────────────────────────────

func (m TimersModel) stopCmd(id int64) tea.Cmd {
	return func() tea.Msg {
		if err := m.db.StopEntry(id); err != nil {
			return ErrMsg(err.Error())
		}
		return tea.BatchMsg{
			func() tea.Msg { return RunningChangedMsg{} },
			func() tea.Msg { return timerEntriesMsg(mustLoadEntries(m.db)) },
			func() tea.Msg { return StatusMsg("Timer stopped") },
		}
	}
}

func (m TimersModel) startCmd(projectID int64, task string) tea.Cmd {
	return func() tea.Msg {
		if err := m.db.StopAllRunning(); err != nil {
			return ErrMsg(err.Error())
		}
		if _, err := m.db.StartEntry(projectID, task); err != nil {
			return ErrMsg(err.Error())
		}
		return tea.BatchMsg{
			func() tea.Msg { return RunningChangedMsg{} },
			func() tea.Msg { return timerEntriesMsg(mustLoadEntries(m.db)) },
			func() tea.Msg { return StatusMsg("Timer started") },
		}
	}
}

func (m TimersModel) deleteCmd(id int64) tea.Cmd {
	return func() tea.Msg {
		if err := m.db.DeleteEntry(id); err != nil {
			return ErrMsg(err.Error())
		}
		return tea.BatchMsg{
			func() tea.Msg { return RunningChangedMsg{} },
			func() tea.Msg { return timerEntriesMsg(mustLoadEntries(m.db)) },
			func() tea.Msg { return StatusMsg("Entry deleted") },
		}
	}
}

func (m TimersModel) toggleInvoiceCmd(e *model.Entry) tea.Cmd {
	return func() tea.Msg {
		newInvoiced := !e.Invoiced
		if !newInvoiced && e.Paid {
			// An entry can't be paid without being invoiced, so un-invoicing
			// a paid entry must also clear Paid — straight back to Pending
			// in one press, rather than leaving a hidden Paid=true,
			// Invoiced=false state that the UI would still badge as PAID.
			// Clear Paid first: SetEntryPaid only takes effect while
			// invoiced=1 still holds.
			if err := m.db.SetEntryPaid(e.ID, false); err != nil {
				return ErrMsg(err.Error())
			}
		}
		if err := m.db.SetEntryInvoiced(e.ID, newInvoiced); err != nil {
			return ErrMsg(err.Error())
		}
		return tea.BatchMsg{
			func() tea.Msg { return timerEntriesMsg(mustLoadEntries(m.db)) },
			func() tea.Msg { return StatusMsg("Invoice status updated") },
		}
	}
}

func (m TimersModel) togglePaidCmd(e *model.Entry) tea.Cmd {
	return func() tea.Msg {
		if err := m.db.SetEntryPaid(e.ID, !e.Paid); err != nil {
			return ErrMsg(err.Error())
		}
		return tea.BatchMsg{
			func() tea.Msg { return timerEntriesMsg(mustLoadEntries(m.db)) },
			func() tea.Msg { return StatusMsg("Payment status updated") },
		}
	}
}

// timerFormValues is a timerForm's fields, parsed and validated —
// everything resolveProject/db writes need, decoupled from the form/input
// widgets themselves. Shared between TimersModel.submitForm and
// ReportsModel.submitEditEntry (see validateTimerForm).
type timerFormValues struct {
	task, clientName, projectName, notes string
	clientID, projectID                  int64
	// Edit mode also lets the start/end time be corrected. New timers
	// always start "now" via startCmd, so these only apply once
	// f.entryID != 0. A blank field means "leave that time as it already
	// is" — the fields come pre-filled when opening an edit, so blank only
	// happens if the user clears one deliberately, and forcing a value in
	// both would make it impossible to touch just one. newStart/newEnd nil
	// means no override; haveEndOverride distinguishes "no override" from
	// "override to nil" (clear End back to running) for End specifically.
	newStart, newEnd *time.Time
	haveEndOverride  bool
}

// validateTimerForm parses and validates a timerForm's fields, returning a
// non-empty errMsg (and a zero/partial timerFormValues) if invalid. Shared
// by TimersModel.submitForm and ReportsModel.submitEditEntry — see
// updateTimerFormFields' doc comment for why this is a free function rather
// than a TimersModel method.
func validateTimerForm(f timerForm) (v timerFormValues, errMsg string) {
	v.task = strings.TrimSpace(f.inputs[fieldTask].Value())
	if v.task == "" {
		return v, "Task description is required"
	}
	v.clientName = strings.TrimSpace(f.inputs[fieldClient].Value())
	v.projectName = strings.TrimSpace(f.inputs[fieldProject].Value())
	v.notes = strings.TrimSpace(f.inputs[fieldNotes].Value())
	v.clientID = f.clientID
	v.projectID = f.projectID

	if f.entryID != 0 {
		if startStr := strings.TrimSpace(f.inputs[fieldStart].Value()); startStr != "" {
			st, ok := parseLocalDateTime(startStr)
			if !ok {
				return v, "Start time must be in YYYY-MM-DD HH:MM format"
			}
			v.newStart = &st
		}
		if endStr := strings.TrimSpace(f.inputs[fieldEnd].Value()); endStr != "" {
			et, ok := parseLocalDateTime(endStr)
			if !ok {
				return v, "End time must be in YYYY-MM-DD HH:MM format"
			}
			v.newEnd = &et
			v.haveEndOverride = true
		}
		if v.newStart != nil && v.newEnd != nil && v.newEnd.Before(*v.newStart) {
			return v, "End time must not be before the start time"
		}
	}
	return v, ""
}

// submitEditEntryCmd returns a tea.Cmd that resolves/creates the
// client+project then applies a validated form's values to the existing
// entry with the given ID. Shared by TimersModel.submitForm's edit branch
// and ReportsModel.submitEditEntry — editing an entry is otherwise
// identical regardless of which view it's driven from. extra are additional
// commands the caller wants batched in alongside the "Entry updated" status
// (e.g. reloading whichever list/report the caller displays).
func submitEditEntryCmd(database *db.DB, entryID int64, v timerFormValues, extra ...tea.Cmd) tea.Cmd {
	return func() tea.Msg {
		resolvedProjectID, err := resolveProject(database, v.clientID, v.clientName, v.projectID, v.projectName)
		if err != nil {
			return ErrMsg(err.Error())
		}

		entries, err := database.ListEntries(0, "", "", true)
		if err != nil {
			return ErrMsg(err.Error())
		}
		var target *model.Entry
		for _, e := range entries {
			if e.ID == entryID {
				target = e
				break
			}
		}
		if target == nil {
			return ErrMsg("Entry not found")
		}
		target.Task = v.task
		target.Notes = v.notes
		if v.newStart != nil {
			target.StartTime = *v.newStart
		}
		if v.haveEndOverride {
			target.EndTime = v.newEnd
		}
		if target.EndTime != nil && target.EndTime.Before(target.StartTime) {
			return ErrMsg("End time must not be before the start time")
		}
		if resolvedProjectID > 0 {
			target.ProjectID = resolvedProjectID
		}
		if err := database.UpdateEntry(target); err != nil {
			return ErrMsg(err.Error())
		}

		batch := make(tea.BatchMsg, 0, len(extra)+1)
		for _, c := range extra {
			batch = append(batch, c)
		}
		batch = append(batch, func() tea.Msg { return StatusMsg("Entry updated") })
		return batch
	}
}

// submitForm resolves/creates the client+project then starts or updates the entry.
func (m TimersModel) submitForm() (TimersModel, tea.Cmd) {
	f := m.form
	v, errMsg := validateTimerForm(f)
	if errMsg != "" {
		m.err = errMsg
		return m, nil
	}

	m.mode = timersModeList
	m.err = ""

	if f.entryID == 0 {
		// New timer
		return m, func() tea.Msg {
			resolvedProjectID, err := resolveProject(m.db, v.clientID, v.clientName, v.projectID, v.projectName)
			if err != nil {
				return ErrMsg(err.Error())
			}
			if err := m.db.StopAllRunning(); err != nil {
				return ErrMsg(err.Error())
			}
			if resolvedProjectID > 0 {
				if _, err := m.db.StartEntry(resolvedProjectID, v.task); err != nil {
					return ErrMsg(err.Error())
				}
			} else {
				// No project — start with a placeholder project
				pid, err := ensureUncategorizedProject(m.db)
				if err != nil {
					return ErrMsg(err.Error())
				}
				if _, err := m.db.StartEntry(pid, v.task); err != nil {
					return ErrMsg(err.Error())
				}
			}
			return tea.BatchMsg{
				func() tea.Msg { return RunningChangedMsg{} },
				func() tea.Msg { return timerEntriesMsg(mustLoadEntries(m.db)) },
				func() tea.Msg { return loadProjectsData(m.db)() },
				func() tea.Msg { return loadClientsCmd(m.db)() },
				func() tea.Msg { return StatusMsg("Timer started") },
			}
		}
	}

	// Edit existing entry
	return m, submitEditEntryCmd(m.db, f.entryID, v,
		func() tea.Msg { return timerEntriesMsg(mustLoadEntries(m.db)) },
		loadProjectsCmd(m.db, 0),
		loadClientsCmd(m.db),
	)
}

// resolveProject finds or creates the client+project and returns the project ID.
// Returns 0 if both clientName and projectName are blank (uncategorized).
func resolveProject(database *db.DB, clientID int64, clientName string, projectID int64, projectName string) (int64, error) {
	// If a project was explicitly selected from the dropdown, use it directly.
	if projectID > 0 {
		return projectID, nil
	}

	// Both blank — uncategorized
	if clientName == "" && projectName == "" {
		return 0, nil
	}

	// Resolve/create client
	if clientID == 0 && clientName != "" {
		clients, err := database.ListClients()
		if err != nil {
			return 0, err
		}
		for _, c := range clients {
			if strings.EqualFold(c.Name, clientName) {
				clientID = c.ID
				break
			}
		}
		if clientID == 0 {
			c, err := database.CreateClient(clientName, 0)
			if err != nil {
				return 0, fmt.Errorf("create client %q: %w", clientName, err)
			}
			clientID = c.ID
		}
	}

	// If only a client was specified with no project name, use client name as project too
	if projectName == "" && clientName != "" {
		projectName = clientName
	}

	// Resolve/create project
	if projectName != "" {
		projects, err := database.ListProjects(clientID)
		if err != nil {
			return 0, err
		}
		for _, p := range projects {
			if strings.EqualFold(p.Name, projectName) {
				return p.ID, nil
			}
		}
		// Create project — if no client exists yet, create one with the project name
		if clientID == 0 {
			c, err := database.CreateClient(projectName, 0)
			if err != nil {
				return 0, fmt.Errorf("create client %q: %w", projectName, err)
			}
			clientID = c.ID
		}
		p, err := database.CreateProject(clientID, projectName)
		if err != nil {
			return 0, fmt.Errorf("create project %q: %w", projectName, err)
		}
		return p.ID, nil
	}

	return 0, nil
}

// ensureUncategorizedProject returns (or creates) a catch-all project for untagged timers.
func ensureUncategorizedProject(database *db.DB) (int64, error) {
	const clientName = "Uncategorized"
	const projectName = "General"

	clients, err := database.ListClients()
	if err != nil {
		return 0, err
	}
	var clientID int64
	for _, c := range clients {
		if c.Name == clientName {
			clientID = c.ID
			break
		}
	}
	if clientID == 0 {
		c, err := database.CreateClient(clientName, 0)
		if err != nil {
			return 0, err
		}
		clientID = c.ID
	}

	projects, err := database.ListProjects(clientID)
	if err != nil {
		return 0, err
	}
	for _, p := range projects {
		if p.Name == projectName {
			return p.ID, nil
		}
	}
	p, err := database.CreateProject(clientID, projectName)
	if err != nil {
		return 0, err
	}
	return p.ID, nil
}

func mustLoadEntries(database *db.DB) []*model.Entry {
	entries, _ := database.ListEntries(0, "", "", true)
	return entries
}

// ── Update: handle openFormMsg ────────────────────────────────────────────────

// We need to handle openFormMsg in the main Update switch.
// Re-open Update to inject it cleanly by adding a case in the top-level switch.
// This is handled below by extending Update via a patch in the Init section.

func (m TimersModel) handleOpenForm(msg openFormMsg) TimersModel {
	f := newTimerForm(msg.clients, msg.projects)
	f.entryID = msg.entryID

	// Pre-populate for edit
	if msg.entryID != 0 {
		for _, e := range m.entries {
			if e.ID == msg.entryID {
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
				break
			}
		}
		f.rebuildClientMatches()
		f.rebuildProjectMatches()
	}

	m.form = f
	if msg.entryID == 0 {
		m.mode = timersModeNew
	} else {
		m.mode = timersModeEdit
	}
	m.err = ""
	return m
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m TimersModel) View() string {
	switch m.mode {
	case timersModeNew, timersModeEdit:
		return m.viewForm()
	default:
		// timersModeConfirmDelete renders inline within viewList (the selected
		// row becomes a "Delete ...?" prompt) rather than replacing the whole
		// screen — deleting one row shouldn't hide the table it belongs to.
		return m.viewList()
	}
}

// ── List view ─────────────────────────────────────────────────────────────────

// renderHeader renders the "Timers  ·  <range>" title line, mirroring
// ReportsModel.renderHeader so the two views read consistently.
func (m TimersModel) renderHeader() string {
	return StyleTitle.Render("Timers") +
		StyleMuted.Render("  ·  ") +
		StyleMuted.Render(m.rangeLabel())
}

// rangeLabel describes the currently active filter in friendly terms. For the
// four quick-filter presets it names the preset outright ("This Week", "All
// time") rather than deriving a label from dateFrom/dateTo — otherwise, once
// the filtered entries happen to fall on a single day (e.g. a fresh install
// with only today's entries so far), every preset would render identically
// as "Today" and switching filters would look like it did nothing. Only a
// custom range — where the specific dates ARE the point — is described by
// its actual from/to dates.
func (m TimersModel) rangeLabel() string {
	switch m.preset {
	case timersPresetToday:
		return "Today"
	case timersPresetYesterday:
		return "Yesterday"
	case timersPresetWeek:
		return "This Week"
	case timersPresetAll:
		return "All time"
	}

	// Custom range. Unlike the in-table day-group headers (always within the
	// visible entries, so recent enough that "Today"/"Yesterday"/"Mon, Jan 2"
	// is unambiguous), a custom range can point anywhere, so non-today/
	// yesterday dates here include the year to stay unambiguous.
	label := func(s string) string {
		t, ok := parseLocalDate(s)
		if !ok {
			return "—"
		}
		friendly := friendlyDay(t)
		if friendly == "Today" || friendly == "Yesterday" {
			return friendly
		}
		return t.Format("Jan 2, 2006")
	}

	switch {
	case m.dateFrom == "" && m.dateTo == "":
		return "All time"
	case m.dateFrom == m.dateTo:
		return label(m.dateFrom)
	default:
		from, to := "—", "—"
		if m.dateFrom != "" {
			from = label(m.dateFrom)
		}
		if m.dateTo != "" {
			to = label(m.dateTo)
		}
		return from + "  →  " + to
	}
}

func (m TimersModel) renderFilterBar() string {
	return renderDateFilterBar(m.fromInput, m.toInput, m.filterIdx)
}

func (m TimersModel) viewList() string {
	header := m.renderHeader()

	if m.mode == timersModeFilter {
		return header + "\n\n" + m.renderFilterBar()
	}

	if len(m.filtered) == 0 {
		return header + "\n\n" + m.viewEmptyState()
	}

	// Window the render lines (day headers + entry rows) so long lists scroll
	// instead of overflowing the view.
	lines := m.buildLines()
	budget := m.rowsBudget()
	m.clampOffset()
	start := m.offset
	if start < 0 {
		start = 0
	}

	// windowEnd accounts for the sticky day header synthesized below when
	// start doesn't already land on one — it takes one of the budgeted
	// slots rather than being added on top of it, so the table's rendered
	// height stays constant whether or not the window happens to start
	// mid-day (see windowEnd's doc comment).
	end := windowEnd(lines, start, budget)
	visible := lines[start:end]

	taskCol, projectCol := m.listColWidths()
	contentW := tColStatus + taskCol + projectCol + tColDuration + tColEarned

	var out []string
	// 1-column blank ahead of "Task" reserves the same slot data rows use
	// for their "•" notes marker (see below), so the header label and the
	// task text beneath it start at the same column either way.
	out = append(out, RowPrefix(false)+
		lipgloss.NewStyle().Background(cBg).Render(" ")+
		StyleTableHeader.Width(taskCol-1).Render("Task")+
		StyleTableHeader.Width(projectCol).Render("Project")+
		StyleTableHeader.Width(tColDuration).Align(lipgloss.Right).Render("Duration")+
		StyleTableHeader.Width(tColStatus).Render("Status")+
		StyleTableHeader.Width(tColEarned).Align(lipgloss.Right).Render("Earned"))

	// divider doubles as a scroll indicator (▲ = more above, ▼ = more below).
	// It spans the gutter too (unlike the header/data rows, which reserve the
	// gutter for the cursor), so its width is the full row width, derived from
	// the actual column widths so it stays aligned even when minimums clamp.
	divW := tColGutter + contentW
	runes := []rune(strings.Repeat("─", divW))
	if start > 0 {
		runes[0] = '▲'
	}
	if end < len(lines) {
		runes[divW-1] = '▼'
	}
	out = append(out, StyleTableDiv.Render(string(runes)))

	// Sticky day header: if the window's top row continues a day whose real
	// header line scrolled off above, synthesize a copy of it here so you
	// can always tell which day you're looking at while scrolling — it
	// switches to the next day's header once that day's own entries reach
	// the top of the window, same as buildLines' real ones do. rowsBudget
	// always reserves a row for this, so it never has to compete with the
	// last entry row for space.
	if len(visible) > 0 && visible[0].kind != timersLineHeader {
		e := m.filtered[visible[0].entryIdx]
		sticky := timersLine{kind: timersLineHeader, day: e.StartTime.Local()}
		out = append(out, m.renderDayHeaderLine(sticky, contentW))
	}

	for _, l := range visible {
		if l.kind == timersLineHeader {
			out = append(out, m.renderDayHeaderLine(l, contentW))
			continue
		}

		e := m.filtered[l.entryIdx]
		sel := l.entryIdx == m.cursor

		var base lipgloss.Style
		switch {
		case sel:
			base = StyleTableRowSel
		case e.IsRunning():
			base = StyleTableRowRun
		default:
			base = StyleTableRow
		}

		// A "•" marks entries with notes — the only other place notes show
		// is the detail pane below (for whichever row is selected) or the
		// edit form, so without this a row's notes are easy to miss unless
		// you happen to select that exact row. It's rendered as its own
		// fixed-style cell (mirrors statusCell below) rather than folded
		// into `task` under `base`: `base` goes Bold when a row is
		// selected, and a bold vs regular glyph can render at a subtly
		// different width in some terminal fonts — enough that the task
		// text visibly shifted by a fraction of a column depending on
		// whether its row was selected. A fixed, never-bold style keeps the
		// marker's width identical either way.
		noteMarker := " "
		if e.Notes != "" {
			noteMarker = "•"
		}
		// rowBg mirrors `base`'s own background (cBgAlt when selected, cBg
		// otherwise) so the fixed-style cells below — which need a color of
		// their own independent of `base` — still complete the selected
		// row's tint instead of leaving a plain-cBg gap in it.
		rowBg := cBg
		if sel {
			rowBg = cBgAlt
		}
		// noteCell is exactly 1 column — `base` already adds its own
		// 1-column left pad via Padding(0,1), which supplies the gap
		// before the task text, so no extra space is added here.
		noteCell := lipgloss.NewStyle().Foreground(cAccent).Background(rowBg).Render(noteMarker)
		task := truncate(e.Task, taskCol-4)
		proj := ""
		if e.Project != nil {
			proj = truncate(e.Project.Client.Name+" › "+e.Project.Name, projectCol-3)
		}
		dur := model.FormatDuration(e.Duration())

		var earned string
		if e.Project == nil || e.Project.Client == nil || e.Project.Client.HourlyRate == 0 {
			earned = "—"
		} else {
			earned = fmt.Sprintf("$%.2f", e.Earnings(e.Project.Client.HourlyRate))
		}

		// The status cell always renders its own fixed foreground (not
		// `base`'s) so the PAID/INVOICED badge — and the muted "pending"
		// text for everything else — keeps its own color regardless of row
		// selection; only its background follows rowBg, to complete the
		// selected row's tint.
		statusCell := lipgloss.NewStyle().Background(rowBg).Width(tColStatus).Render(statusLabel(e, rowBg))

		out = append(out, RowPrefix(sel)+
			noteCell+
			base.Width(taskCol-1).Render(task)+
			base.Width(projectCol).Render(proj)+
			base.Width(tColDuration).Align(lipgloss.Right).Render(dur)+
			statusCell+
			base.Width(tColEarned).Align(lipgloss.Right).Render(earned))
	}

	out = append(out, m.renderFooterTotals(contentW)...)

	tableStr := strings.Join(out, "\n")
	detail := m.renderDetailPane()

	body := header + "\n\n" + tableStr + "\n\n" + detail

	if m.mode == timersModeConfirmDelete {
		if e := m.selectedEntry(); e != nil {
			body = centerOverlay(body, renderConfirmModal(fmt.Sprintf(`Delete "%s"?`, truncate(e.Task, 44))))
		}
	}

	return body
}

// renderDayHeaderLine renders a day-group separator: just the friendly day
// label, aligned to the same gutter+content width as the entry rows below
// it. See the "Day grouping" comment above buildLines for why this doesn't
// carry a per-day total any more.
func (m TimersModel) renderDayHeaderLine(l timersLine, contentW int) string {
	label := StylePrimary.Render(friendlyDay(l.day))
	gap := contentW - lipgloss.Width(label)
	if gap < 0 {
		gap = 0
	}
	blank := lipgloss.NewStyle().Background(cBg).Render(strings.Repeat(" ", gap))
	return RowPrefix(false) + label + blank
}

// renderFooterTotals renders the table's closing divider and a single grand
// total row combining every currently filtered entry, regardless of how many
// distinct days it spans — e.g. selecting "This Week" shows one combined
// total here rather than a separate one under each day.
func (m TimersModel) renderFooterTotals(contentW int) []string {
	dur, earned := m.filteredTotals()
	totals := StyleMuted.Render("Total  ") +
		StyleSuccess.Render(model.FormatDuration(dur)) +
		StyleMuted.Render("   ·   ") +
		StyleSuccess.Render(fmt.Sprintf("$%.2f", earned))

	gap := contentW - lipgloss.Width(totals)
	if gap < 0 {
		gap = 0
	}
	blank := lipgloss.NewStyle().Background(cBg).Render(strings.Repeat(" ", gap))

	divW := tColGutter + contentW
	divider := StyleTableDiv.Render(strings.Repeat("─", divW))

	return []string{divider, RowPrefix(false) + blank + totals}
}

func (m TimersModel) viewEmptyState() string {
	if len(m.entries) == 0 {
		return StyleTitle.Render("No entries yet.") + "\n\n" +
			StyleMuted.Render("Press n to start your first timer.") + "\n" +
			StyleMuted.Render("Client and project can be created right from the form.")
	}
	return StyleTitle.Render("No entries for this range.") + "\n\n" +
		StyleMuted.Render("Press t for today, a for all time, or f to pick a custom range.")
}

func (m TimersModel) renderDetail(e *model.Entry) string {
	// date line
	started := e.StartTime.Local().Format("Jan 2, 2006  3:04 PM")
	dateLine := StyleMuted.Render("Started  ") + StyleTableRow.Render(started)
	if e.EndTime != nil {
		dateLine += StyleMuted.Render("  –  ") + StyleTableRow.Render(e.EndTime.Local().Format("3:04 PM"))
	}

	notesLine := StyleMuted.Render(noteDetailLabel) + formatNoteDetail(e.Notes, usableWidth(m.width))
	statusLine := StyleMuted.Render("Status   ") + invoiceLabel(e)

	return strings.Join([]string{dateLine, notesLine, statusLine}, "\n")
}

// ── Form view ─────────────────────────────────────────────────────────────────

func (m TimersModel) viewForm() string {
	saveLabel := "Save & Start"
	if m.form.entryID != 0 {
		saveLabel = "Save"
	}
	return viewTimerForm(m.form, m.err, saveLabel)
}

// viewTimerForm renders a timer/entry form — shared by TimersModel's own
// New/Edit Timer form and ReportsModel's edit-entry view (see
// updateTimerFormFields' doc comment for why the form logic lives as free
// functions rather than TimersModel methods). saveLabel is parametrized
// because "Save & Start" only makes sense for Timers' own new-timer flow;
// editing a past entry from Reports doesn't start anything running.
func viewTimerForm(f timerForm, errMsg string, saveLabel string) string {
	title := "New Timer"
	if f.entryID != 0 {
		title = "Edit Entry"
	}

	var b strings.Builder
	b.WriteString(StyleTitle.Render(title) + "\n\n")

	b.WriteString(f.renderFormField("Task", fieldTask) + "\n\n")

	b.WriteString(f.renderFormField("Client", fieldClient) + "\n")
	if f.focusIdx == fieldClient && f.showClientDrop && len(f.clientMatches) > 0 {
		b.WriteString(renderDropdown(f.clientMatches, f.clientSel) + "\n")
	}
	b.WriteString("\n")

	b.WriteString(f.renderFormField("Project", fieldProject) + "\n")
	if f.focusIdx == fieldProject && f.showProjectDrop && len(f.projectMatches) > 0 {
		b.WriteString(renderDropdown(f.projectMatches, f.projectSel) + "\n")
	}
	b.WriteString("\n")

	if f.entryID != 0 {
		b.WriteString(f.renderFormField("Start", fieldStart) + "\n\n")
		b.WriteString(f.renderFormField("End", fieldEnd) + "\n\n")
	}

	b.WriteString(f.renderFormField("Notes", fieldNotes) + "\n\n")

	saveBtn := StyleButtonPrimary
	if f.focusIdx == fieldSave {
		saveBtn = StyleButtonActive
	}
	cancelBtn := StyleButton
	if f.focusIdx == fieldCancel {
		cancelBtn = StyleButtonActive
	}
	b.WriteString(saveBtn.Render(saveLabel))
	b.WriteString(lipgloss.NewStyle().Background(cBg).Render("   "))
	b.WriteString(cancelBtn.Render("Cancel"))

	if errMsg != "" {
		b.WriteString("\n\n" + StyleDanger.Render("✖  "+errMsg))
	}

	b.WriteString("\n\n")
	if f.entryID != 0 {
		b.WriteString(StyleHelp.Render("Times use YYYY-MM-DD HH:MM (24h). Leave End blank to keep the timer running.") + "\n")
	}
	b.WriteString(StyleHelp.Render("Client and project are created automatically if they don't exist."))

	return b.String()
}

// renderFormField is a timerForm method (not a TimersModel one) so
// ReportsModel's edit-entry form can render fields identically — see
// updateTimerFormFields' doc comment for why these stay decoupled from
// TimersModel specifically.
func (f timerForm) renderFormField(label string, idx int) string {
	focused := f.focusIdx == idx
	ti := f.inputs[idx]
	refreshInputStyle(&ti)

	var labelStr string
	if focused {
		labelStr = StyleFormLabelFocused.Render(label)
	} else {
		labelStr = StyleFormLabel.Render(label)
	}

	view := trimTrailingUnstyledPad(ti.View())
	var inputStr string
	if focused {
		inputStr = StyleFormInputFocused.Width(48).Render(view)
	} else {
		inputStr = StyleFormInput.Width(48).Render(view)
	}

	// The input box is 2 lines (text + its bottom-border underline), but
	// labelStr is only 1 — padCellToTwoLines pads it to match with an
	// explicit cBg-styled blank line rather than letting JoinHorizontal
	// auto-pad the gap, which produces a genuinely unstyled row (see its
	// doc comment; same bug this fixed on the date-range filter bar).
	return lipgloss.JoinHorizontal(lipgloss.Top, padCellToTwoLines(labelStr), inputStr)
}

func renderDropdown(items []dropdownItem, sel int) string {
	start := sel - 4
	if start < 0 {
		start = 0
	}
	end := start + 7
	if end > len(items) {
		end = len(items)
	}

	var lines []string
	for i := start; i < end; i++ {
		item := items[i]
		prefix := "  "
		var s lipgloss.Style
		if i == sel {
			prefix = "▌ "
			s = StyleTableRowSel.Width(50)
		} else if item.id == 0 {
			s = StylePrimary.Width(50)
		} else {
			s = StyleMuted.Width(50)
		}
		lines = append(lines, s.Render(prefix+item.label))
	}

	return lipgloss.NewStyle().
		Background(cBg).
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(cPrimary).
		// BorderBackground is a separate property from Background — see
		// renderConfirmModal's comment for the same rule — so without this
		// the left border bar shows the terminal's default color instead
		// of cBg.
		BorderBackground(cBg).
		PaddingLeft(StyleFormLabel.GetWidth()).
		Render(strings.Join(lines, "\n"))
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// statusLabel renders the entry's billing status. PAID/INVOICED get a
// colored badge; pending (the unflagged default — same term as invoiceLabel's
// default case, in the detail pane below the table) gets plain muted text
// rather than a badge, so the two flagged states still stand out and pending
// rows don't read as if every entry needs attention.
// statusLabel renders the entry's status cell. The PAID/INVOICED cases are
// badges with their own fixed fill color, unaffected by row selection by
// design (same as elsewhere); the plain "pending" text has no fill of its
// own, so it takes bg explicitly to match the row's own background (cBgAlt
// when selected, cBg otherwise — see statusCell's rowBg) instead of always
// baking in cBg and leaving a mismatched patch behind the word when selected.
func statusLabel(e *model.Entry, bg lipgloss.Color) string {
	switch {
	case e.Paid:
		return renderBadge("PAID", cGreen)
	case e.Invoiced:
		return renderBadge("INVOICED", cAccent)
	default:
		return lipgloss.NewStyle().Foreground(cDim).Background(bg).Render("pending")
	}
}

func invoiceLabel(e *model.Entry) string {
	switch {
	case e.Paid:
		return renderBadge("PAID", cGreen)
	case e.Invoiced:
		return renderBadge("INVOICED", cAccent)
	default:
		return StyleMuted.Render("pending  ") + StyleHelp.Render("i invoice · p paid")
	}
}

func billingStr(e *model.Entry) string {
	if e.Project == nil || e.Project.Client == nil || e.Project.Client.HourlyRate == 0 {
		return StyleMuted.Render("—")
	}
	return fmt.Sprintf("$%.2f", e.Earnings(e.Project.Client.HourlyRate))
}

var _ = time.Second
