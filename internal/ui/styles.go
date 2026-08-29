package ui

// ── Layout constants ──────────────────────────────────────────────────────────

// MaxContentWidth is the maximum width of the centered content column.
const MaxContentWidth = 150

// ChromeRows is the number of rows consumed by the top bar, hotkey bar, and
// footer — each renders 3 rows (verified directly against
// renderTopBar/renderHotkeyBar/renderFooter's actual output, not derived
// from their padding settings on paper: those have drifted from this
// comment before, which is exactly the kind of mismatch that silently
// over-allocates content height and causes it to overflow past the real
// terminal size). Total chrome = 3 + 3 + 3 = 9.
const ChromeRows = 9

// ContentWidth returns the actual content width clamped to MaxContentWidth.
func ContentWidth(termWidth int) int {
	if termWidth < MaxContentWidth {
		return termWidth
	}
	return MaxContentWidth
}

// ContentPad returns the left-padding needed to center MaxContentWidth within termWidth.
func ContentPad(termWidth int) int {
	if termWidth <= MaxContentWidth {
		return 0
	}
	return (termWidth - MaxContentWidth) / 2
}
