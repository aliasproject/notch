package views

import (
	"regexp"
	"strings"
	"testing"

	"github.com/aliasproject/notch/internal/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// runeKey builds a tea.KeyMsg for a single-character keybinding like "y" or "d".
func runeKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

var csiRE = regexp.MustCompile(`\x1b\[([0-9;?]*)([A-Za-z@])`)

// bgColorRE pulls a truecolor background (48;2;r;g;b) out of an SGR params
// string, wherever it falls among other combined attributes (e.g.
// "1;38;2;...;48;2;1;1;1" — bold + foreground + background all in one code,
// which is how lipgloss emits a style with multiple attributes set).
var bgColorRE = regexp.MustCompile(`48;2;(\d+;\d+;\d+)`)

// forceTrueColor forces the TrueColor profile for the duration of the test,
// restoring the previous one on cleanup. Some lipgloss behavior — notably
// Style.Width()'s word-wrap decision — renders differently under `go test`'s
// default no-TTY (colorless) profile than it does in a real terminal, so a
// test exercising that needs this even when it isn't asserting on color
// itself (see TestRenderDateFilterBar_NoWrapWhenFocusedWithCursorAtEnd).
func forceTrueColor(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

// unstyledCellCount forces the TrueColor profile (styles render as no-op
// plain text otherwise, e.g. under `go test` with no TTY) and counts visible,
// non-newline characters in render()'s output that render with no active
// background SGR code. This is the exact defect class this session found
// repeatedly: lipgloss.JoinHorizontal/JoinVertical auto-pad a block that's
// shorter than its neighbors with genuinely unstyled blank runs, which show
// the terminal's own default color instead of the app's background once
// concatenated with properly-styled fragments (see padCellToTwoLines and
// renderConfirmModal's BorderBackground comment for the two variants of it).
//
// It takes a render callback rather than an already-rendered string:
// SetColorProfile only affects .Render() calls made after it, so the target
// string must be built (or rebuilt) here, after the profile is forced —
// passing in a string rendered under the default (often colorless, no-TTY)
// profile would silently make every cell look "unstyled".
func unstyledCellCount(t *testing.T, render func() string) int {
	t.Helper()
	forceTrueColor(t)
	s := render()

	bgActive := false
	count := 0
	i := 0
	for i < len(s) {
		if s[i] == 0x1b {
			if loc := csiRE.FindStringSubmatchIndex(s[i:]); loc != nil && loc[0] == 0 {
				params := s[i+loc[2] : i+loc[3]]
				final := s[i+loc[4] : i+loc[5]]
				if final == "m" {
					switch {
					case params == "" || params == "0":
						bgActive = false
					case strings.Contains(";"+params, "48;") || strings.HasPrefix(params, "48"):
						bgActive = true
					}
				}
				i += loc[1]
				continue
			}
			i++
			continue
		}
		if s[i] != '\n' && s[i] != '\r' && !bgActive {
			count++
		}
		i++
	}
	return count
}

// activeBackgroundAt scans a truecolor-rendered string (see forceTrueColor)
// and returns the "r;g;b" truecolor background active at the first
// occurrence of needle, or "" if no background is active there (or needle
// isn't found at all).
func activeBackgroundAt(s, needle string) string {
	bg := ""
	i := 0
	for i < len(s) {
		if s[i] == 0x1b {
			if loc := csiRE.FindStringSubmatchIndex(s[i:]); loc != nil && loc[0] == 0 {
				params := s[i+loc[2] : i+loc[3]]
				final := s[i+loc[4] : i+loc[5]]
				if final == "m" {
					switch {
					case params == "" || params == "0":
						bg = ""
					default:
						if m := bgColorRE.FindStringSubmatch(params); m != nil {
							bg = m[1]
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
			return bg
		}
		i++
	}
	return ""
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		name string
		s    string
		max  int
		want string
	}{
		{"empty string", "", 5, ""},
		{"zero max", "hello", 0, ""},
		{"negative max", "hello", -3, ""},
		{"max of one", "hello", 1, "h"},
		{"fits exactly", "hello", 5, "hello"},
		{"shorter than max", "hi", 10, "hi"},
		{"truncates with ellipsis", "hello world", 5, "hell…"},
		{"rune-aware truncation", "héllo world", 3, "hé…"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := truncate(c.s, c.max); got != c.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", c.s, c.max, got, c.want)
			}
		})
	}
}

func TestUsableWidth(t *testing.T) {
	cases := []struct {
		w    int
		want int
	}{
		{0, 0},
		{6, 6},
		{7, 1},
		{20, 14},
		{100, 94},
	}
	for _, c := range cases {
		if got := usableWidth(c.w); got != c.want {
			t.Errorf("usableWidth(%d) = %d, want %d", c.w, got, c.want)
		}
	}
}

func TestMaxInt(t *testing.T) {
	cases := []struct{ a, b, want int }{
		{1, 2, 2},
		{2, 1, 2},
		{-1, -5, -1},
		{3, 3, 3},
	}
	for _, c := range cases {
		if got := maxInt(c.a, c.b); got != c.want {
			t.Errorf("maxInt(%d, %d) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// TestRenderDateFilterBar_LabelAlignsWithTextLine is a regression test:
// JoinHorizontal's automatic padding for a mismatched-height block rounds an
// odd 1-row shortfall to the *top* under Center alignment, which put the
// "From"/"To" labels on the input box's border line instead of its text
// line. It should render as exactly 2 lines, with the labels and date text
// together on line 0 and only the border dashes on line 1.
func TestRenderDateFilterBar_LabelAlignsWithTextLine(t *testing.T) {
	from := makeTextInput("YYYY-MM-DD", 10, dateFilterInputWidth)
	from.SetValue("2024-01-01")
	to := makeTextInput("YYYY-MM-DD", 10, dateFilterInputWidth)
	to.SetValue("2024-01-31")

	got := renderDateFilterBar(from, to, 0)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("renderDateFilterBar produced %d lines, want 2 (text line + border line):\n%s", len(lines), got)
	}

	textLine, borderLine := lines[0], lines[1]
	for _, want := range []string{"From", "To", "2024-01-01", "2024-01-31"} {
		if !strings.Contains(textLine, want) {
			t.Errorf("line 0 (text line) missing %q, got %q", want, textLine)
		}
		if strings.Contains(borderLine, want) {
			t.Errorf("line 1 (border line) should not contain %q, got %q", want, borderLine)
		}
	}
	if !strings.Contains(borderLine, "─") {
		t.Errorf("line 1 (border line) should contain the input boxes' underline, got %q", borderLine)
	}
}

// TestRenderDateFilterBar_NoWrapWhenFocusedWithCursorAtEnd is a regression
// test for a real overflow bug: textinput.Model renders its own cursor
// regardless of focus state, and when the cursor sits exactly at the end of
// a value at CharLimit, its computed width (value + cursor + padding) comes
// out 1 column past what its own Width budget asks for (a textinput quirk,
// not something we can fix from the style side). Since
// lipgloss.Style.Width() *word-wraps* content wider than it rather than
// just failing to pad, that overflow used to split the date value itself
// across two lines instead of one — dateFilterInputWidth (1 less than the
// box) exists specifically to absorb it.
func TestRenderDateFilterBar_NoWrapWhenFocusedWithCursorAtEnd(t *testing.T) {
	// Style.Width()'s word-wrap decision (the bug this reproduces) behaves
	// differently under go test's default no-TTY profile than in a real
	// terminal — force TrueColor so this actually exercises it.
	forceTrueColor(t)
	from := makeTextInput("YYYY-MM-DD", 10, dateFilterInputWidth)
	from.SetValue("2024-01-01") // at CharLimit, like any real typed date
	from.Focus()
	to := makeTextInput("YYYY-MM-DD", 10, dateFilterInputWidth)
	to.SetValue("2024-01-31")

	got := renderDateFilterBar(from, to, 0)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("renderDateFilterBar produced %d lines, want 2 — the focused date value wrapped across extra lines instead of staying on one:\n%s", len(lines), got)
	}
	if !strings.Contains(lines[0], "2024-01-01") {
		t.Errorf("line 0 should contain the full unwrapped date, got %q", lines[0])
	}
}

// TestRenderDateFilterBar_NoUnstyledBackgroundGap covers the whole family of
// missing-background bugs found and fixed on this bar this session: the
// label/box height mismatch, textinput's own unstyled prompt/padding/cursor,
// and the wrap-induced split above (a wrap always introduces at least one
// unstyled seam, since the wrapped-off fragment lands outside every styled
// span built for the single-line case).
func TestRenderDateFilterBar_NoUnstyledBackgroundGap(t *testing.T) {
	from := makeTextInput("YYYY-MM-DD", 10, dateFilterInputWidth)
	from.SetValue("2024-01-01")
	from.Focus()
	to := makeTextInput("YYYY-MM-DD", 10, dateFilterInputWidth)
	to.SetValue("2024-01-31")

	var got string
	n := unstyledCellCount(t, func() string {
		got = renderDateFilterBar(from, to, 0)
		return got
	})
	if n > 0 {
		t.Errorf("renderDateFilterBar has %d unstyled cell(s), want 0:\n%s", n, got)
	}
}

func TestRowPrefix(t *testing.T) {
	selected := RowPrefix(true)
	unselected := RowPrefix(false)

	if !strings.Contains(selected, "▌") {
		t.Errorf("RowPrefix(true) = %q, want it to contain the cursor glyph", selected)
	}
	if strings.Contains(unselected, "▌") {
		t.Errorf("RowPrefix(false) = %q, want it to not contain the cursor glyph", unselected)
	}
	if lipgloss.Width(selected) != lipgloss.Width(unselected) {
		t.Errorf("RowPrefix widths differ: selected=%d unselected=%d, want equal so rows don't shift",
			lipgloss.Width(selected), lipgloss.Width(unselected))
	}
}

// ── Button styles ────────────────────────────────────────────────────────────

// withTestPalette sets theme.Colors to a known Palette for the duration of
// the test (restoring the previous one after) and rebuilds every views
// Style* var from it, the same way a live theme change does via RefreshTheme.
func withTestPalette(t *testing.T, p theme.Palette) {
	t.Helper()
	prev := theme.Colors
	t.Cleanup(func() {
		theme.Colors = prev
		RefreshTheme()
	})
	theme.Colors = p
	RefreshTheme()
}

// TestButtonStyles_TextStaysLegibleAgainstAccentFill guards against two bugs
// found in the same session: StyleButtonPrimary originally paired a light
// accent fill with light (cText) button text — legible only when the theme's
// accent happened to be darker than its text color, which pastel-on-dark
// themes routinely aren't. The fix mirrors StyleButtonActive's already-
// working pattern (accent background + cBg foreground) rather than
// swapping in a different *background* (e.g. cBgAlt): cBg is the one color a
// theme is guaranteed to keep far from every accent color (an accent that
// didn't contrast with the base background would make the whole desktop
// theme illegible, not just this button), where a "slightly elevated
// surface" shade like cBgAlt is not guaranteed to be far from cBg itself.
func TestButtonStyles_TextStaysLegibleAgainstAccentFill(t *testing.T) {
	// A pastel-on-dark palette where every accent sits close in brightness
	// to Text — exactly the case that broke the original StyleButtonPrimary.
	withTestPalette(t, theme.Palette{
		Primary: "#91b0de", Accent: "#9dc6e9", Success: "#99c2ed",
		Warning: "#a4cbf7", Danger: "#c79ea9", Text: "#c2d9e9",
		Dim: "#b7d2e5", Subtle: "#899eac", Bg: "#0d171f",
		BgAlt: "#0f1720", Border: "#5f6468", Highlight: "#afcfff",
	})

	for _, tt := range []struct {
		name  string
		style lipgloss.Style
		fill  lipgloss.Color
	}{
		{"StyleButtonPrimary", StyleButtonPrimary, cPrimary},
		{"StyleButtonActive", StyleButtonActive, cGreen},
		{"StyleButtonDanger", StyleButtonDanger, cRed},
	} {
		if got := tt.style.GetForeground(); got != lipgloss.TerminalColor(cBg) {
			t.Errorf("%s.GetForeground() = %v, want cBg (%v)", tt.name, got, cBg)
		}
		if got := tt.style.GetBackground(); got != lipgloss.TerminalColor(tt.fill) {
			t.Errorf("%s.GetBackground() = %v, want its accent fill (%v)", tt.name, got, tt.fill)
		}
	}
}

// TestButtonStyles_BackgroundNeverMatchesPageBackground guards against a
// button silently blending into the page: with the fix above in place, this
// mostly re-confirms Primary/Active/Danger are never filled with cBg itself,
// across a theme where the "elevated surface" shade (cBgAlt) sits close
// enough to cBg that using it for a button (an earlier, reverted fix) would
// have made the button hard to spot against the page.
func TestButtonStyles_BackgroundNeverMatchesPageBackground(t *testing.T) {
	withTestPalette(t, theme.Palette{
		Primary: "#668691", Accent: "#afe6ea", Success: "#a3cdc8",
		Warning: "#cdfef3", Danger: "#849d8e", Text: "#e5e4e8",
		Dim: "#acabae", Subtle: "#5d6466", Bg: "#020d10",
		BgAlt: "#0a0f11", Border: "#5d6466", Highlight: "#749daa",
	})

	for name, sty := range map[string]lipgloss.Style{
		"StyleButtonPrimary": StyleButtonPrimary,
		"StyleButtonActive":  StyleButtonActive,
		"StyleButtonDanger":  StyleButtonDanger,
	} {
		if got := sty.GetBackground(); got == lipgloss.TerminalColor(cBg) {
			t.Errorf("%s.GetBackground() = %v, matches cBg — button would be invisible against the page", name, got)
		}
	}
}
