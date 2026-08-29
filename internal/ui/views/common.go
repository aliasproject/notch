package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/aliasproject/notch/internal/db"
	"github.com/aliasproject/notch/internal/model"
	"github.com/aliasproject/notch/internal/theme"
)

// ── Palette ───────────────────────────────────────────────────────────────────
//
// Colors (and the Style* vars built from them below) are populated by
// RefreshTheme, not by these var declarations directly, so a running app can
// pick up an edited theme.conf without a restart — see RefreshTheme.

var (
	cPrimary   lipgloss.Color
	cAccent    lipgloss.Color
	cGreen     lipgloss.Color
	cAmber     lipgloss.Color
	cRed       lipgloss.Color
	cText      lipgloss.Color
	cDim       lipgloss.Color
	cSubtle    lipgloss.Color
	cBg        lipgloss.Color
	cBgAlt     lipgloss.Color
	cBorder    lipgloss.Color
	cHighlight lipgloss.Color
)

// ── Shared message types ──────────────────────────────────────────────────────

type StatusMsg string
type ErrMsg string
type RunningChangedMsg struct{}
type ReloadMsg struct{}

// Hotkey is one key/label pair returned by a view's Help(), for display in
// the app-level hotkey bar (see app.go renderHotkeyBar). Key holds the raw
// key name(s) (e.g. "n", "tab / shift+tab") which the bar renders inside its
// own boxed cap, separate from Label's plain descriptive text.
type Hotkey struct {
	Key   string
	Label string
}

// ── Shared load message types ─────────────────────────────────────────────────

type clientsLoadedMsg struct{ clients []*model.Client }
type projectsLoadedMsg struct{ projects []*model.Project }
type timerEntriesMsg []*model.Entry

// ── Shared commands ───────────────────────────────────────────────────────────

func StatusCmd(s string) tea.Cmd {
	return func() tea.Msg { return StatusMsg(s) }
}

func ErrCmd(s string) tea.Cmd {
	return func() tea.Msg { return ErrMsg(s) }
}

func RunningChangedCmd() tea.Cmd {
	return func() tea.Msg { return RunningChangedMsg{} }
}

func loadClientsCmd(database *db.DB) tea.Cmd {
	return func() tea.Msg {
		clients, err := database.ListClients()
		if err != nil {
			return ErrMsg(fmt.Sprintf("load clients: %v", err))
		}
		return clientsLoadedMsg{clients: clients}
	}
}

func loadProjectsCmd(database *db.DB, clientID int64) tea.Cmd {
	return func() tea.Msg {
		projects, err := database.ListProjects(clientID)
		if err != nil {
			return ErrMsg(fmt.Sprintf("load projects: %v", err))
		}
		return projectsLoadedMsg{projects: projects}
	}
}

func loadTimerEntriesCmd(database *db.DB) tea.Cmd {
	return func() tea.Msg {
		entries, err := database.ListEntries(0, "", "", true)
		if err != nil {
			return ErrMsg(fmt.Sprintf("load entries: %v", err))
		}
		return timerEntriesMsg(entries)
	}
}

// ── Shared key maps ───────────────────────────────────────────────────────────

type listKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	New    key.Binding
	Edit   key.Binding
	Delete key.Binding
}

var listKeys = listKeyMap{
	Up:     key.NewBinding(key.WithKeys("up", "k")),
	Down:   key.NewBinding(key.WithKeys("down", "j")),
	New:    key.NewBinding(key.WithKeys("n")),
	Edit:   key.NewBinding(key.WithKeys("e")),
	Delete: key.NewBinding(key.WithKeys("d")),
}

type formKeyMap struct {
	Submit    key.Binding
	Cancel    key.Binding
	NextField key.Binding
	PrevField key.Binding
}

var formKeys = formKeyMap{
	Submit:    key.NewBinding(key.WithKeys("enter")),
	Cancel:    key.NewBinding(key.WithKeys("esc")),
	NextField: key.NewBinding(key.WithKeys("tab")),
	PrevField: key.NewBinding(key.WithKeys("shift+tab")),
}

// ── Styles ────────────────────────────────────────────────────────────────────
//
// Rule: NO background colors on table rows or list items.
// Selection is shown with a left gutter cursor character only.
// This prevents the "dark block" artifact from width-padded cells.

var (
	// Typography
	StyleTitle    lipgloss.Style
	StyleSubtitle lipgloss.Style
	StyleMuted    lipgloss.Style
	StyleDim      lipgloss.Style
	StyleDanger   lipgloss.Style
	StyleWarning  lipgloss.Style
	StyleSuccess  lipgloss.Style
	StyleRunning  lipgloss.Style
	StylePrimary  lipgloss.Style
	StyleAccent   lipgloss.Style
	StyleHelp     lipgloss.Style

	// Table
	StyleTableHeader lipgloss.Style
	StyleTableRow    lipgloss.Style
	StyleTableRowDim lipgloss.Style
	StyleTableRowSel lipgloss.Style
	StyleTableRowRun lipgloss.Style
	StyleTableDiv    lipgloss.Style

	// Forms
	StyleFormLabel        lipgloss.Style
	StyleFormLabelFocused lipgloss.Style
	StyleFormInput        lipgloss.Style
	StyleFormInputFocused lipgloss.Style

	// Buttons
	StyleButton        lipgloss.Style
	StyleButtonPrimary lipgloss.Style
	StyleButtonActive  lipgloss.Style
	StyleButtonDanger  lipgloss.Style

	// Panels — subtle border, no filled background
	StylePanel        lipgloss.Style
	StylePanelFocused lipgloss.Style
)

func init() { RefreshTheme() }

// RefreshTheme rebuilds the palette and every Style* var from theme.Colors.
// Called once at startup (via init) and again whenever the app detects the
// user's theme config file has changed (see theme.CheckReload, wired up in
// app.go's TickMsg handler) — every render already goes through these Style
// vars, so reassigning them here is all it takes for a running app to pick
// up an edited theme without a restart.
func RefreshTheme() {
	cPrimary = theme.Colors.Primary
	cAccent = theme.Colors.Accent
	cGreen = theme.Colors.Success
	cAmber = theme.Colors.Warning
	cRed = theme.Colors.Danger
	cText = theme.Colors.Text
	cDim = theme.Colors.Dim
	cSubtle = theme.Colors.Subtle
	cBg = theme.Colors.Bg
	cBgAlt = theme.Colors.BgAlt
	cBorder = theme.Colors.Border
	cHighlight = theme.Colors.Highlight

	// Every style below carries an explicit Background(cBg): each .Render() call
	// ends with its own SGR reset, so a fragment without a background would show
	// the terminal's default background instead of cBg once concatenated with
	// other styled fragments on the same line (see the table rule below).
	StyleTitle = lipgloss.NewStyle().Foreground(cText).Background(cBg).Bold(true)
	StyleSubtitle = lipgloss.NewStyle().Foreground(cDim).Background(cBg)
	StyleMuted = lipgloss.NewStyle().Foreground(cDim).Background(cBg)
	StyleDim = lipgloss.NewStyle().Foreground(cSubtle).Background(cBg)
	StyleDanger = lipgloss.NewStyle().Foreground(cRed).Background(cBg)
	StyleWarning = lipgloss.NewStyle().Foreground(cAmber).Background(cBg)
	StyleSuccess = lipgloss.NewStyle().Foreground(cGreen).Background(cBg)
	StyleRunning = lipgloss.NewStyle().Foreground(cGreen).Background(cBg).Bold(true)
	StylePrimary = lipgloss.NewStyle().Foreground(cPrimary).Background(cBg).Bold(true)
	StyleAccent = lipgloss.NewStyle().Foreground(cAccent).Background(cBg)
	StyleHelp = lipgloss.NewStyle().Foreground(cSubtle).Background(cBg)

	// Table — solid navy background on every cell so SGR resets inside the
	// rendered cells can't punch holes in the column background.
	// Padding(0, 1) gives one space of breathing room on each side of every cell.
	StyleTableHeader = lipgloss.NewStyle().Foreground(cDim).Bold(true).Padding(0, 1).Background(cBg)
	StyleTableRow = lipgloss.NewStyle().Foreground(cText).Padding(0, 1).Background(cBg)
	StyleTableRowDim = lipgloss.NewStyle().Foreground(cDim).Padding(0, 1).Background(cBg)
	// Selection reads as a lifted row (a subtle cBgAlt tint spanning the
	// whole row, text staying its normal color) rather than recoloring the
	// text — the "▌" cursor bar (see RowPrefix) is the one accent-colored
	// element, so selection doesn't compete with the app's actual use of
	// color (status badges, earnings, etc.) for attention.
	StyleTableRowSel = lipgloss.NewStyle().Foreground(cText).Bold(true).Padding(0, 1).Background(cBgAlt)
	StyleTableRowRun = lipgloss.NewStyle().Foreground(cGreen).Bold(true).Padding(0, 1).Background(cBg)
	StyleTableDiv = lipgloss.NewStyle().Foreground(cBorder).Background(cBg)

	// Forms
	StyleFormLabel = lipgloss.NewStyle().Foreground(cDim).Background(cBg).Width(16).PaddingRight(1)
	StyleFormLabelFocused = lipgloss.NewStyle().Foreground(cAccent).Background(cBg).Bold(true).Width(16).PaddingRight(1)
	StyleFormInput = lipgloss.NewStyle().
		Foreground(cText).
		Background(cBg).
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(cBorder).
		// BorderBackground is a separate property from Background — the
		// border glyph itself doesn't inherit it (see renderConfirmModal's
		// comment for the same rule), so without this the underline shows
		// the terminal's default color instead of cBg.
		BorderBackground(cBg).
		Width(40)
	StyleFormInputFocused = lipgloss.NewStyle().
		Foreground(cText).
		Background(cBg).
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(cPrimary).
		BorderBackground(cBg).
		Width(40)

	// Buttons
	StyleButton = lipgloss.NewStyle().
		Foreground(cText).
		Background(cSubtle).
		Padding(0, 3).
		Bold(true)
	// Foreground is cBg, not cText: cPrimary/cGreen/cRed are theme accent
	// colors, which in a pastel-on-dark theme can be close in brightness to
	// cText (light-on-light — poor contrast, and the original bug report
	// here). cBg is by construction the theme's darkest color, so it stays
	// legible against an accent fill regardless of how light or dark that
	// particular theme's accent happens to be — the same reasoning
	// StyleButtonActive already relied on before either of these did.
	// (A cBgAlt/cSubtle *background* was tried instead at one point, to
	// keep cText as the button's foreground — reverted because a theme
	// whose "elevated surface" shade sits close to cBg made the button
	// visually blend into the page instead of reading as a button. An
	// accent-colored fill can't have that problem: cPrimary/cGreen/cRed are
	// never close to cBg, unlike cBgAlt in a low-elevation theme.)
	StyleButtonPrimary = lipgloss.NewStyle().
		Foreground(cBg).
		Background(cPrimary).
		Padding(0, 3).
		Bold(true)
	StyleButtonActive = lipgloss.NewStyle().
		Foreground(cBg).
		Background(cGreen).
		Padding(0, 3).
		Bold(true)
	StyleButtonDanger = lipgloss.NewStyle().
		Foreground(cBg).
		Background(cRed).
		Padding(0, 3).
		Bold(true)

	// Panels — subtle border, no filled background
	StylePanel = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cBorder).
		Padding(1, 2)
	StylePanelFocused = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cPrimary).
		Padding(1, 2)
}

// ── Gutter cursor ─────────────────────────────────────────────────────────────

const cursorOn  = "▌ "
const cursorOff = "  "

// RowPrefix returns the gutter cursor for a list row. Both variants are
// background-styled: lipgloss drops leading *unstyled* whitespace when it
// re-renders a long line inside the width-padded body column.
func RowPrefix(selected bool) string {
	if selected {
		// Background(cBgAlt) here (rather than cBg) so the row's highlight
		// tint starts right at the gutter, under the cursor bar itself,
		// instead of leaving a plain-cBg sliver before the tinted cells
		// begin — see StyleTableRowSel for the rest of that tint.
		return lipgloss.NewStyle().Foreground(cPrimary).Background(cBgAlt).Render(cursorOn)
	}
	return lipgloss.NewStyle().Background(cBg).Render(cursorOff)
}

// ── Badges ────────────────────────────────────────────────────────────────────

// renderBadge renders a compact status badge as a flat rectangle — plain
// Background/Padding, no cap glyphs.
func renderBadge(text string, bg lipgloss.Color) string {
	return lipgloss.NewStyle().
		Foreground(cBg).
		Background(bg).
		Bold(true).
		Padding(0, 1).
		Render(text)
}

// ── Shared utilities ──────────────────────────────────────────────────────────

func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 1 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + "…"
}

// entryDetailLines is the fixed height of an entry detail pane (Started/
// Notes/Status — see TimersModel.renderDetail and
// ReportsModel.renderEntryDetail). Both must always emit exactly this many
// lines, note or no note, so that rowsBudget/entriesRowsBudget's table
// window never grows or shrinks depending on which row happens to be
// selected.
const entryDetailLines = 3

// noteDetailLabel is the label prefix for the Notes line in an entry detail
// pane, shared so formatNoteDetail's width math matches what's actually
// rendered.
const noteDetailLabel = "Notes    "

// formatNoteDetail renders a detail pane's Notes value as a single line: a
// muted "—" placeholder when there's no note, otherwise the note collapsed
// to one line and truncated to fit width. The detail pane's height is
// fixed (see entryDetailLines), so this line must never wrap — a note long
// enough to wrap under the outer content column's Width() would otherwise
// grow the pane past its reserved height and push the header off-screen.
func formatNoteDetail(notes string, width int) string {
	if notes == "" {
		return StyleMuted.Render("—")
	}
	notes = strings.Join(strings.Fields(notes), " ")
	// StyleTableRow carries its own Padding(0, 1) — 2 columns (one on each
	// side) on top of the text itself — so the budget has to leave room for
	// that too, not just the label prefix, or a note sized to exactly fill
	// the remaining width overflows by those 2 columns and wraps under the
	// outer content column's Width().
	max := width - len([]rune(noteDetailLabel)) - 2
	if max < 1 {
		max = 1
	}
	return StyleTableRow.Render(truncate(notes, max))
}

func renderConfirmDelete(target string, _ int) string {
	// Plain "\n" join, not lipgloss.JoinVertical: JoinVertical pads shorter
	// lines to the width of the widest one using *unstyled* spaces, which
	// would punch a hole in the background between these lines of differing
	// width. The outer content column (see app.go) fills the rest correctly.
	return StyleDanger.Bold(true).Render("Delete "+target+"?") + "\n\n" +
		StyleMuted.Render("This cannot be undone.") + "\n\n" +
		StyleHelp.Render("y  confirm    esc  cancel")
}

// ── Modal overlay ─────────────────────────────────────────────────────────────
//
// Neither bubbletea nor lipgloss ships a modal/dialog widget — this is the
// standard DIY pattern: render the background and a small bordered box
// separately, then splice the box's lines into the background's lines at a
// given position. ansi.Cut is escape-code-aware (unlike a raw string slice),
// so cutting a styled background line mid-run doesn't corrupt its color
// codes on either side of the pasted box.

// renderConfirmModal renders a small bordered "are you sure?" dialog meant to
// be composited over a view with centerOverlay, rather than replacing that
// view's content outright — the table (or form) stays visible around it.
func renderConfirmModal(title string) string {
	content := StyleDanger.Bold(true).Render(title) + "\n\n" +
		StyleMuted.Render("This cannot be undone.") + "\n\n" +
		StyleHelp.Render("y  confirm    esc  cancel")
	return lipgloss.NewStyle().
		Background(cBg).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cRed).
		// BorderBackground is a separate property from Background — the
		// border glyphs don't inherit it, so without this the box's own
		// border cells show the terminal's default color (see the footer
		// border comment in app.go for the same rule).
		BorderBackground(cBg).
		Width(44).
		Padding(2, 4).
		Render(content)
}

// overlay composites fg on top of bg at the given (col, row) cell offset.
// bg lines shorter than col are padded with plain spaces first; fg rows past
// the end of bg are dropped rather than growing the background.
func overlay(bg, fg string, col, row int) string {
	bgLines := strings.Split(bg, "\n")
	fgLines := strings.Split(fg, "\n")

	for i, fgLine := range fgLines {
		r := row + i
		if r < 0 || r >= len(bgLines) {
			continue
		}
		bgLine := bgLines[r]
		bgWidth := ansi.StringWidth(bgLine)

		left := ansi.Cut(bgLine, 0, col)
		if lw := ansi.StringWidth(left); lw < col {
			// bgLine is shorter than the overlay's left edge (e.g. a blank
			// separator row, or a detail line that doesn't span the full
			// column width) — pad with cBg-backed spaces, not bare ones, or
			// the gap shows the terminal's own default color instead of the
			// app's background.
			left += lipgloss.NewStyle().Background(cBg).Render(strings.Repeat(" ", col-lw))
		}
		var right string
		if end := col + ansi.StringWidth(fgLine); end < bgWidth {
			right = ansi.Cut(bgLine, end, bgWidth)
		}
		bgLines[r] = left + fgLine + right
	}
	return strings.Join(bgLines, "\n")
}

// centerOverlay composites fg centered within bg's own bounding box (widest
// line × line count) — not the terminal window, just whatever bg happens to
// contain, so the modal centers over the content that's actually on screen.
func centerOverlay(bg, fg string) string {
	bgLines := strings.Split(bg, "\n")
	bgWidth := 0
	for _, l := range bgLines {
		if w := ansi.StringWidth(l); w > bgWidth {
			bgWidth = w
		}
	}
	fgLines := strings.Split(fg, "\n")
	fgWidth := 0
	for _, l := range fgLines {
		if w := ansi.StringWidth(l); w > fgWidth {
			fgWidth = w
		}
	}
	col := maxInt(0, (bgWidth-fgWidth)/2)
	row := maxInt(0, (len(bgLines)-len(fgLines))/2)
	return overlay(bg, fg, col, row)
}

// padCellToTwoLines turns a single-line, already-background-styled cell into
// a 2-line block matching the bordered input boxes' height (text line +
// border line): the content on top, a cBg-styled blank of the same width
// below. Building every cell at the box's height up front — rather than
// joining mismatched heights and letting lipgloss pad the gap — matters
// because lipgloss.JoinHorizontal pads shorter blocks with genuinely empty
// strings, not anything background-styled, so any auto-padded row shows the
// terminal's default color instead of cBg. Center alignment compounds that:
// on an odd 1-row shortfall it rounds the padding to the *top*, which is why
// the label used to land on the border row instead of the text row above it.
func padCellToTwoLines(top string) string {
	w := lipgloss.Width(top)
	blank := lipgloss.NewStyle().Background(cBg).Width(w).Render("")
	return top + "\n" + blank
}

// dateFilterBoxWidth is renderDateFilterBar's From/To box width.
// dateFilterInputWidth is the ti.Width passed to makeTextInput for those
// same fields — 1 less than the box, not equal to it: with a value at
// CharLimit and the cursor sitting at the end (the common case for a typed
// date), textinput.Model's own render computes value+cursor+padding as
// Width+1, one column past what it asks for. Keeping the box a column wider
// than ti.Width absorbs that overflow instead of triggering
// lipgloss.Style.Width's word-wrap, which — since it wraps rather than just
// failing to pad when content is too wide — used to split the date value
// itself across two lines. Both constants live together so a future resize
// of one doesn't silently reopen the gap between them.
const (
	dateFilterBoxWidth   = 14
	dateFilterInputWidth = dateFilterBoxWidth - 1
)

// renderDateFilterBar renders a From/To date-range editor: two bordered date
// inputs plus inline hotkey hints. Shared by any view with a date-range
// filter (Reports and Timers) so their filter UI stays visually identical.
func renderDateFilterBar(fromInput, toInput textinput.Model, filterIdx int) string {
	refreshInputStyle(&fromInput)
	refreshInputStyle(&toInput)

	// StyleFormInput/StyleFormInputFocused already carry the Background +
	// BorderBackground this box needs (see RefreshTheme) — reuse them
	// instead of re-declaring the same bordered-box style inline without
	// those, which was leaving the underline (and the gap above it) showing
	// the terminal's default color instead of cBg.
	fromBox, toBox := StyleFormInput, StyleFormInput
	if filterIdx == 0 {
		fromBox = StyleFormInputFocused
	} else {
		toBox = StyleFormInputFocused
	}

	// Every piece here is built to exactly 2 lines (matching the boxes'
	// text-line + border-line height) before the final join, so that join
	// never needs to auto-pad a shorter block — see padCellToTwoLines.
	fromField := lipgloss.JoinHorizontal(lipgloss.Top,
		padCellToTwoLines(StyleFormLabel.Render("From")),
		fromBox.Width(dateFilterBoxWidth).Render(trimTrailingUnstyledPad(fromInput.View())),
	)
	toField := lipgloss.JoinHorizontal(lipgloss.Top,
		padCellToTwoLines(StyleFormLabel.Render("To  ")),
		toBox.Width(dateFilterBoxWidth).Render(trimTrailingUnstyledPad(toInput.View())),
	)

	return lipgloss.JoinHorizontal(lipgloss.Top,
		fromField,
		padCellToTwoLines(StyleMuted.Render("    ")),
		toField,
		padCellToTwoLines(StyleHelp.Render("    tab switch  ·  enter apply  ·  esc cancel")),
	)
}

func makeTextInput(placeholder string, charLimit, width int) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.CharLimit = charLimit
	ti.Width = width
	// textinput.Model defaults to a "> " prompt, which its own Width budget
	// doesn't account for (Width sizes the value/cursor/padding only — the
	// prompt is prepended after that, unbudgeted). Every call site here
	// already shows an external label ("From", "Task", ...) to the left of
	// the box, so the prompt is redundant — and worse, it silently pushes
	// the real rendered width past the outer box's own Width(), which
	// word-wraps rather than just failing to pad, splitting the value
	// itself across two lines. Clearing it removes both the redundant
	// glyph and the overflow.
	ti.Prompt = ""
	refreshInputStyle(&ti)
	return ti
}

// refreshInputStyle (re)applies the current theme colors to a
// textinput.Model's internal styles. textinput.Model does its own internal
// width-padding (independent of whatever outer lipgloss style later wraps
// ti.View()), styled by these fields — left at their zero-value default they
// carry no background, so that internal padding — and the "> " prompt, and
// the placeholder text itself — would show the terminal's default color
// instead of cBg. (The one case this can't fix is the placeholder view's own
// trailing pad, which the library appends with no style wrapper at all —
// trimTrailingUnstyledPad works around that at the call site.)
//
// Unlike the package's Style* vars, a textinput.Model's styles aren't
// recomputed by RefreshTheme — each one is a value stored inside some
// model's own state (a form's []textinput.Model field), not a shared
// package var RefreshTheme has a reference to. So every render call site
// that calls ti.View() calls this immediately before, to pick up the
// current cBg/cText/cSubtle rather than whatever was live when the input
// was first constructed — otherwise an input created before a live theme
// switch would keep rendering with the old colors indefinitely.
func refreshInputStyle(ti *textinput.Model) {
	ti.PromptStyle = lipgloss.NewStyle().Background(cBg)
	ti.TextStyle = lipgloss.NewStyle().Foreground(cText).Background(cBg)
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(cSubtle).Background(cBg)
	// Cursor.Style backs the visible ("blink on") cursor block, rendered
	// with Reverse(true) on top of it — left unset, Reverse just inverts
	// the terminal's own default colors instead of the theme's. Setting it
	// here makes the reversed block cText-on-cBg (i.e. it renders as a
	// solid cText block, matching the app's text color) instead of
	// whatever the terminal defaults to. TextStyle backs the "blink off"
	// state, which renders the character plainly with no reverse.
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(cText).Background(cBg)
	ti.Cursor.TextStyle = lipgloss.NewStyle().Foreground(cText).Background(cBg)
}

// trimTrailingUnstyledPad strips a textinput.View()'s trailing run of bare
// (un-styled) padding spaces before it's wrapped in an outer Width()-styled
// box. textinput pads its own rendered value up to its configured Width
// using styles set in makeTextInput — except the empty-placeholder path,
// which appends that trailing pad with no style wrapper at all, unfixable
// from the style side. Since the outer box's own Width() padding is always
// applied last and is correctly cBg-styled, trimming the raw run here and
// letting that outer padding re-fill the same space produces an identical
// width with correct styling, in both the typed-value and empty-placeholder
// cases.
func trimTrailingUnstyledPad(view string) string {
	return strings.TrimRight(view, " ")
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// usableWidth returns the width available for view content inside the
// centered content column, accounting for the column's horizontal padding.
func usableWidth(w int) int {
	if w <= 6 {
		return w
	}
	return w - 6
}

// hRule draws a horizontal line using the border color.
func hRule(width int) string {
	return StyleTableDiv.Render(lipgloss.NewStyle().Width(width).Render(""))
}
