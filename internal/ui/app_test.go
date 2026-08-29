package ui

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/aliasproject/notch/internal/db"
	"github.com/aliasproject/notch/internal/theme"
	"github.com/aliasproject/notch/internal/ui/views"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func newTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestTruncateStr(t *testing.T) {
	cases := []struct {
		name string
		s    string
		max  int
		want string
	}{
		{"empty string", "", 5, ""},
		{"negative max, non-empty string", "hello", -1, ""},
		{"zero max, non-empty string", "hello", 0, ""},
		{"zero max, empty string", "", 0, ""},
		{"fits exactly", "hello", 5, "hello"},
		{"shorter than max", "hi", 10, "hi"},
		{"truncates with ellipsis", "hello world", 5, "hell…"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := truncateStr(c.s, c.max); got != c.want {
				t.Errorf("truncateStr(%q, %d) = %q, want %q", c.s, c.max, got, c.want)
			}
		})
	}
}

// runeKey builds a tea.KeyMsg for a single-character keybinding like "f" or "q".
func runeKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func TestQuit_AlwaysEscapesBusyMode(t *testing.T) {
	// Enter timers filter mode via the exported Update path (pure, no DB touch)
	// so the model is genuinely "busy" without reaching into unexported views fields.
	busyTimers, _ := views.NewTimers(nil).Update(runeKey('f'))
	if !busyTimers.IsBusy() {
		t.Fatal("expected timers model to be busy after pressing 'f' (filter mode)")
	}

	m := AppModel{activeTab: TabTimers, timers: busyTimers}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c while busy: want a quit cmd, got nil")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("ctrl+c while busy: want tea.QuitMsg, got %T", msg)
	}

	// "q" must behave the same way.
	next, cmd2 := m.Update(runeKey('q'))
	m2 := next.(AppModel)
	if cmd2 == nil {
		t.Fatal("'q' while busy: want a quit cmd, got nil")
	}
	if _, ok := cmd2().(tea.QuitMsg); !ok {
		t.Errorf("'q' while busy: want tea.QuitMsg, got %T", cmd2())
	}
	if m2.activeTab != TabTimers {
		t.Errorf("quit should not otherwise mutate model state, activeTab = %d", m2.activeTab)
	}
}

func TestTabSwitching(t *testing.T) {
	d := newTestDB(t)
	m, err := New(d)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	cases := []struct {
		key  rune
		want int
	}{
		{'2', TabProjects},
		{'3', TabClients},
		{'4', TabReports},
		{'1', TabTimers},
	}
	for _, c := range cases {
		var cmd tea.Cmd
		var next tea.Model
		next, cmd = m.Update(runeKey(c.key))
		m = next.(AppModel)
		if m.activeTab != c.want {
			t.Errorf("after pressing %q, activeTab = %d, want %d", c.key, m.activeTab, c.want)
		}
		if cmd == nil {
			t.Errorf("after pressing %q, want a non-nil Init cmd for the newly active tab", c.key)
		}
	}
}

func TestBusyTab_IgnoresTabSwitchKeys(t *testing.T) {
	busyTimers, _ := views.NewTimers(nil).Update(runeKey('f'))
	m := AppModel{activeTab: TabTimers, timers: busyTimers}

	next, _ := m.Update(runeKey('2'))
	got := next.(AppModel)
	if got.activeTab != TabTimers {
		t.Errorf("tab switch key while busy: activeTab = %d, want it to stay on TabTimers (%d)", got.activeTab, TabTimers)
	}
}

func TestRenderBars_MatchWidth(t *testing.T) {
	d := newTestDB(t)
	m, err := New(d)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for _, w := range []int{40, 80, 120, 200} {
		m.width = w
		m.height = 40
		if got := lipgloss.Width(m.renderTopBar()); got != w {
			t.Errorf("renderTopBar width at m.width=%d = %d, want %d", w, got, w)
		}
		if got := lipgloss.Width(m.renderHotkeyBar()); got != w {
			t.Errorf("renderHotkeyBar width at m.width=%d = %d, want %d", w, got, w)
		}
		if got := lipgloss.Width(m.renderFooter()); got != w {
			t.Errorf("renderFooter width at m.width=%d = %d, want %d", w, got, w)
		}
	}
}

// TestRenderTopBar_PlainWordmarkNoIcon guards a deliberate design choice: the
// top bar used to render "⏱  Notch" in the bright accent color; it's now a
// plain "Notch" wordmark, since the clock glyph read as clutter rather than
// identity and the accent color is reserved for things that are actually
// interactive (the active tab, buttons).
func TestRenderTopBar_PlainWordmarkNoIcon(t *testing.T) {
	d := newTestDB(t)
	m, err := New(d)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m.width, m.height = 80, 40

	got := m.renderTopBar()
	if !strings.Contains(got, "Notch") {
		t.Errorf("renderTopBar() missing wordmark, got %q", got)
	}
	if strings.Contains(got, "⏱") {
		t.Errorf("renderTopBar() still contains the clock icon, want a plain wordmark: %q", got)
	}
}

// appCsiRE/fgColorRE/forceTrueColor/activeForegroundAt/withTestPalette mirror
// the equivalent helpers in internal/ui/views/common_test.go — duplicated
// rather than shared since the two are separate packages and this is the
// only place internal/ui needs them.
var appCsiRE = regexp.MustCompile(`\x1b\[([0-9;?]*)([A-Za-z@])`)
var fgColorRE = regexp.MustCompile(`38;2;(\d+;\d+;\d+)`)

func forceTrueColor(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

// activeForegroundAt scans a truecolor-rendered string (see forceTrueColor)
// and returns the "r;g;b" truecolor foreground active at the first
// occurrence of needle, or "" if none is active there (or needle isn't found
// at all).
func activeForegroundAt(s, needle string) string {
	fg := ""
	i := 0
	for i < len(s) {
		if s[i] == 0x1b {
			if loc := appCsiRE.FindStringSubmatchIndex(s[i:]); loc != nil && loc[0] == 0 {
				params := s[i+loc[2] : i+loc[3]]
				final := s[i+loc[4] : i+loc[5]]
				if final == "m" {
					switch {
					case params == "" || params == "0":
						fg = ""
					default:
						if m := fgColorRE.FindStringSubmatch(params); m != nil {
							fg = m[1]
						}
					}
				}
				i += loc[1]
				continue
			}
			i++
			continue
		}
		if strings.HasPrefix(s[i:], needle) {
			return fg
		}
		i++
	}
	return ""
}

// withTestPalette sets theme.Colors to a known Palette for the duration of
// the test (restoring the previous one after) and rebuilds every app.go and
// views Style*/appColor* var from it, the same way a live theme change does.
func withTestPalette(t *testing.T, p theme.Palette) {
	t.Helper()
	prev := theme.Colors
	t.Cleanup(func() {
		theme.Colors = prev
		refreshAppTheme()
		views.RefreshTheme()
	})
	theme.Colors = p
	refreshAppTheme()
	views.RefreshTheme()
}

// TestRenderTopBar_WordmarkUsesInactiveTabColor guards the wordmark's color
// choice: it went through two revisions in the same session (bright
// accent -> plain cText -> this) because cText itself still read as "one of
// the brighter colors" against some themes. appColorDim is the same color
// already used for inactive tab labels, so the wordmark now reads as a
// quiet, de-emphasized element rather than competing with the active tab or
// any button for attention.
func TestRenderTopBar_WordmarkUsesInactiveTabColor(t *testing.T) {
	withTestPalette(t, theme.Palette{
		Primary: "#111111", Accent: "#222222", Success: "#333333",
		Warning: "#444444", Danger: "#555555", Text: "#e5e4e8",
		Dim: "#acabae", Subtle: "#5d6466", Bg: "#020d10",
		BgAlt: "#0a0f11", Border: "#5d6466", Highlight: "#666666",
	})

	d := newTestDB(t)
	m, err := New(d)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m.width, m.height = 80, 40

	forceTrueColor(t)
	got := m.renderTopBar()
	if fg := activeForegroundAt(got, "Notch"); fg != "172;171;174" {
		t.Errorf("wordmark foreground = %q, want 172;171;174 (Dim, matching the inactive-tab color)", fg)
	}
}

// TestChromeRows_MatchesActualBarHeights is a regression test: the
// ChromeRows constant (used to compute each view's usable content height —
// contentDims subtracts it from the terminal height) had drifted from its
// own doc comment's derivation before, and views quietly trusted whichever
// number was actually in the constant. If ChromeRows ever again
// under-counts the real combined height of the three bars, every view's
// content gets more height budget than truly exists, overflows past the
// real terminal size, and can push earlier content (even the top bar
// itself) off-screen via the terminal's own scrolling — exactly the
// mechanism behind a real bug found in the reports entries view. This
// checks the constant against the bars' actual rendered height directly,
// not against a hand-derived number that can go stale the same way the
// comment did.
func TestChromeRows_MatchesActualBarHeights(t *testing.T) {
	d := newTestDB(t)
	m, err := New(d)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m.width, m.height = 100, 40

	top := lipgloss.Height(m.renderTopBar())
	hotkey := lipgloss.Height(m.renderHotkeyBar())
	footer := lipgloss.Height(m.renderFooter())
	total := top + hotkey + footer

	if total != ChromeRows {
		t.Errorf("actual chrome height = %d (top=%d hotkey=%d footer=%d), but ChromeRows = %d",
			total, top, hotkey, footer, ChromeRows)
	}
}

// TestRenderBody_MatchWidth is a regression test: renderBody's content
// column is centered via two gutters each sized by ContentPad, which
// truncates (termWidth-MaxContentWidth)/2. Reusing that same truncated value
// for both gutters left the whole row 1 column short of termWidth whenever
// that difference is odd — matching TestRenderBars_MatchWidth's bars (which
// all render at the untouched Width(m.width)) only by coincidence, on the
// widths that happen to produce an even difference. The width list here
// deliberately includes both parities on both sides of MaxContentWidth (150).
func TestRenderBody_MatchWidth(t *testing.T) {
	d := newTestDB(t)
	m, err := New(d)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for _, w := range []int{40, 80, 120, 150, 151, 152, 155, 200, 201, 251} {
		m.width = w
		m.height = 40
		if got := lipgloss.Width(m.renderBody()); got != w {
			t.Errorf("renderBody width at m.width=%d = %d, want %d", w, got, w)
		}
	}
}

func TestRenderFooter_ShowsRunningTimer(t *testing.T) {
	d := newTestDB(t)
	c, err := d.CreateClient("Acme", 100)
	if err != nil {
		t.Fatal(err)
	}
	p, err := d.CreateProject(c.ID, "Website")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.StartEntry(p.ID, "build feature"); err != nil {
		t.Fatal(err)
	}

	m, err := New(d)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// 120 rather than a narrower width: the running pill's caps + extra
	// padding eat into the task-name budget, so a narrower width would
	// truncate "build feature" before this assertion even gets to check it.
	m.width = 120

	footer := m.renderFooter()
	if !strings.Contains(footer, "build feature") {
		t.Errorf("renderFooter() should show the running entry's task, got: %q", footer)
	}
}

// TestReportsEntriesView_LongNoteDoesNotOverflow is a regression test: a
// long note used to be rendered on its own, unbounded line (no Width, no
// truncation), which could word-wrap under the outer content column's
// Width() and grow the frame past the terminal's actual height — pushing
// the top bar off-screen via bubbletea's own top-line-dropping (see
// standardRenderer.flush, which keeps only the last r.height lines when a
// frame is taller than the terminal). This drives the full app (not just
// ReportsModel in isolation) through switching to the Reports tab and
// opening its entries view with a maximal-length note, and checks the
// fully assembled frame never exceeds the terminal height across a range
// of realistic window sizes, including short ones.
func TestReportsEntriesView_LongNoteDoesNotOverflow(t *testing.T) {
	d := newTestDB(t)
	c, err := d.CreateClient("Acme", 100)
	if err != nil {
		t.Fatal(err)
	}
	p, err := d.CreateProject(c.ID, "Web")
	if err != nil {
		t.Fatal(err)
	}
	e, err := d.StartEntry(p.ID, "task with a long note")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.StopEntry(e.ID); err != nil {
		t.Fatal(err)
	}
	entries, err := d.ListEntries(0, "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	e = entries[0]
	e.Notes = strings.Repeat("a very long note that would wrap around multiple times if not truncated properly. ", 6)
	if err := d.UpdateEntry(e); err != nil {
		t.Fatal(err)
	}

	for _, sz := range []struct{ w, h int }{{80, 24}, {100, 24}, {100, 30}, {120, 40}, {100, 18}, {70, 20}} {
		m, err := New(d)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		var model tea.Model = m
		model, _ = model.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})

		// Switch to the Reports tab, then open the entries view for the
		// selected row — both trigger async load commands that must be
		// executed and fed back through Update for m.rows to populate.
		var cmd tea.Cmd
		model, cmd = model.Update(runeKey('4'))
		if cmd != nil {
			model, _ = model.Update(cmd())
		}
		model, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if cmd != nil {
			model, _ = model.Update(cmd())
		}

		out := model.View()
		if h := lipgloss.Height(out); h > sz.h {
			t.Errorf("size=%dx%d: rendered frame height %d exceeds terminal height %d — top bar would be pushed off-screen", sz.w, sz.h, h, sz.h)
		}
	}
}

func TestRenderFooter_IdleWhenNoRunningEntry(t *testing.T) {
	d := newTestDB(t)
	m, err := New(d)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m.width = 100

	footer := m.renderFooter()
	if !strings.Contains(footer, "idle") {
		t.Errorf("renderFooter() should show 'idle' with no running entry, got: %q", footer)
	}
}
