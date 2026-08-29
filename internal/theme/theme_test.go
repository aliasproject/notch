package theme

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// saveGlobalState snapshots the package vars CheckReload mutates and restores
// them after the test, so tests can freely set up "as if we already loaded
// from this file" state without bleeding into other tests. It also blanks
// lastOSThemePath so tests run in isolation from whatever aliasos theme file
// (if any) happens to exist on the machine running the tests; tests that
// specifically exercise OS-theme behavior set it back explicitly.
func saveGlobalState(t *testing.T) {
	t.Helper()
	path, mtime, have, colors := lastLoadedPath, lastLoadedMTime, haveFile, Colors
	osPath, osMtime, haveOS := lastOSThemePath, lastOSThemeMTime, haveOSThemeFile
	t.Cleanup(func() {
		lastLoadedPath, lastLoadedMTime, haveFile, Colors = path, mtime, have, colors
		lastOSThemePath, lastOSThemeMTime, haveOSThemeFile = osPath, osMtime, haveOS
	})
	lastOSThemePath, lastOSThemeMTime, haveOSThemeFile = "", time.Time{}, false
}

func writeConfig(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "theme.conf")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// ── LoadFrom ─────────────────────────────────────────────────────────────────

func TestLoadFrom_AppliesValidOverrides(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "primary = #FF0000\n# a comment\n\naccent=#00FF00\n")

	p := LoadFrom(path)
	if p.Primary != "#FF0000" {
		t.Errorf("Primary = %v, want #FF0000", p.Primary)
	}
	if p.Accent != "#00FF00" {
		t.Errorf("Accent = %v, want #00FF00", p.Accent)
	}
	// Fields not mentioned in the file keep their default value.
	if p.Success != Default.Success {
		t.Errorf("Success = %v, want unchanged default %v", p.Success, Default.Success)
	}
}

func TestLoadFrom_MissingFileReturnsDefault(t *testing.T) {
	p := LoadFrom(filepath.Join(t.TempDir(), "does-not-exist.conf"))
	if p != Default {
		t.Errorf("LoadFrom(missing file) = %+v, want Default", p)
	}
}

func TestLoadFrom_MalformedLinesAreSkippedIndividually(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, ""+
		"primary = not-a-hex\n"+ // invalid hex, skipped
		"no-equals-sign-here\n"+ // no '=', skipped
		"unknown_key = #123456\n"+ // unrecognized key, skipped
		"bg = #0f172a\n") // valid, applied

	p := LoadFrom(path)
	if p.Primary != Default.Primary {
		t.Errorf("Primary = %v, want default %v (invalid hex should be ignored)", p.Primary, Default.Primary)
	}
	if p.Bg != "#0f172a" {
		t.Errorf("Bg = %v, want #0f172a (valid override should still apply despite other bad lines)", p.Bg)
	}
}

func TestLoadFrom_AllKnownKeys(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, ""+
		"primary=#111111\n"+
		"accent=#222222\n"+
		"success=#333333\n"+
		"warning=#444444\n"+
		"danger=#555555\n"+
		"text=#666666\n"+
		"dim=#777777\n"+
		"subtle=#888888\n"+
		"bg=#999999\n"+
		"bg_alt=#aaaaaa\n"+
		"border=#bbbbbb\n"+
		"highlight=#cccccc\n")

	p := LoadFrom(path)
	want := Palette{
		Primary: "#111111", Accent: "#222222", Success: "#333333",
		Warning: "#444444", Danger: "#555555", Text: "#666666",
		Dim: "#777777", Subtle: "#888888", Bg: "#999999",
		BgAlt: "#aaaaaa", Border: "#bbbbbb", Highlight: "#cccccc",
	}
	if p != want {
		t.Errorf("LoadFrom = %+v, want %+v", p, want)
	}
}

// ── CheckReload ──────────────────────────────────────────────────────────────

func TestCheckReload_NoopWhenUnchanged(t *testing.T) {
	saveGlobalState(t)
	dir := t.TempDir()
	path := writeConfig(t, dir, "primary = #111111\n")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	lastLoadedPath = path
	lastLoadedMTime = info.ModTime()
	haveFile = true
	Colors = LoadFrom(path)

	if CheckReload() {
		t.Error("CheckReload() should be a no-op when the file hasn't changed since the last load")
	}
}

func TestCheckReload_ReloadsOnNewerMTime(t *testing.T) {
	saveGlobalState(t)
	dir := t.TempDir()
	path := writeConfig(t, dir, "primary = #111111\n")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	lastLoadedPath = path
	lastLoadedMTime = info.ModTime()
	haveFile = true
	Colors = LoadFrom(path)

	writeConfig(t, dir, "primary = #222222\n")
	// Force the mtime forward explicitly so this doesn't flake on filesystems
	// with coarse timestamp granularity.
	future := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}

	if !CheckReload() {
		t.Fatal("CheckReload() should report a change after the file is rewritten with a newer mtime")
	}
	if Colors.Primary != "#222222" {
		t.Errorf("Colors.Primary = %v, want #222222", Colors.Primary)
	}
}

func TestCheckReload_RevertsToDefaultOnDeletion(t *testing.T) {
	saveGlobalState(t)
	dir := t.TempDir()
	path := writeConfig(t, dir, "primary = #333333\n")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	lastLoadedPath = path
	lastLoadedMTime = info.ModTime()
	haveFile = true
	Colors = LoadFrom(path)

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	if !CheckReload() {
		t.Fatal("CheckReload() should report a change when the config file is removed")
	}
	if Colors != Default {
		t.Errorf("Colors after deletion = %+v, want Default", Colors)
	}

	// A second check with the file still gone should now be a no-op — not
	// re-report "changed" forever.
	if CheckReload() {
		t.Error("CheckReload() should stop reporting changes once already reverted to Default")
	}
}

func TestCheckReload_NoopWithNoConfiguredPath(t *testing.T) {
	saveGlobalState(t)
	lastLoadedPath = ""

	if CheckReload() {
		t.Error("CheckReload() should be a no-op when no config path was resolved")
	}
}

// ── OS theme (aliasos) ───────────────────────────────────────────────────────

func writeOSTheme(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "colors.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestPaletteFromOSTheme_RichFormatUsesNamedShades(t *testing.T) {
	kv := map[string]string{
		"background": "#0d171f", "foreground": "#b7d2e5", "accent": "#91b0de",
		"light_fg": "#c2d9e9", "dark_fg": "#899eac", "muted": "#5f6468",
		"lighter_bg": "#252e35", "green": "#99c2ed", "yellow": "#a4cbf7",
		"red": "#c79ea9", "cyan": "#9dc6e9", "bright_blue": "#afcfff",
	}

	p, ok := paletteFromOSTheme(kv)
	if !ok {
		t.Fatal("paletteFromOSTheme() ok = false, want true")
	}
	want := Palette{
		Bg: "#0d171f", Primary: "#91b0de", Text: "#c2d9e9", Dim: "#899eac",
		Subtle: "#5f6468", Border: "#5f6468", BgAlt: "#252e35",
		Success: "#99c2ed", Warning: "#a4cbf7", Danger: "#c79ea9",
		Accent: "#9dc6e9", Highlight: "#afcfff",
	}
	if p != want {
		t.Errorf("paletteFromOSTheme = %+v, want %+v", p, want)
	}
}

func TestPaletteFromOSTheme_MinimalFormatDerivesMissingShades(t *testing.T) {
	// Only the base trio a theme is guaranteed to define — no muted, no
	// lighter_bg, no named hues.
	kv := map[string]string{
		"background": "#000000",
		"foreground": "#ffffff",
		"accent":     "#4080c0",
	}

	p, ok := paletteFromOSTheme(kv)
	if !ok {
		t.Fatal("paletteFromOSTheme() ok = false, want true")
	}
	if p.Bg != "#000000" || p.Primary != "#4080c0" || p.Text != "#ffffff" {
		t.Fatalf("base fields wrong: %+v", p)
	}
	// Derived grays should sit strictly between Bg and Text, in order.
	toInt := func(c lipgloss.Color) int {
		r, g, b, _ := hexChannels(string(c))
		return r + g + b
	}
	bg, border, bgAlt, subtle, dim, text := toInt(p.Bg), toInt(p.Border), toInt(p.BgAlt), toInt(p.Subtle), toInt(p.Dim), toInt(p.Text)
	if !(bg < bgAlt && bgAlt < border && border < subtle && subtle < dim && dim < text) {
		t.Errorf("derived ramp not monotonic: bg=%d bgAlt=%d border=%d subtle=%d dim=%d text=%d", bg, bgAlt, border, subtle, dim, text)
	}
	// No named hues, so semantic colors fall back to the accent.
	if p.Success != p.Primary || p.Warning != p.Primary || p.Danger != p.Primary || p.Accent != p.Primary || p.Highlight != p.Primary {
		t.Errorf("semantic colors should fall back to Primary when unnamed: %+v", p)
	}
}

func TestPaletteFromOSTheme_MissingBackgroundOrForegroundFails(t *testing.T) {
	if _, ok := paletteFromOSTheme(map[string]string{"foreground": "#ffffff"}); ok {
		t.Error("paletteFromOSTheme() ok = true without background, want false")
	}
	if _, ok := paletteFromOSTheme(map[string]string{"background": "#000000"}); ok {
		t.Error("paletteFromOSTheme() ok = true without foreground, want false")
	}
}

func TestCheckReload_PicksUpOSThemeChange(t *testing.T) {
	saveGlobalState(t)
	dir := t.TempDir()
	path := writeOSTheme(t, dir, "background = \"#000000\"\nforeground = \"#ffffff\"\naccent = \"#111111\"\n")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	lastOSThemePath = path
	lastOSThemeMTime = info.ModTime()
	haveOSThemeFile = true
	Colors = Default

	writeOSTheme(t, dir, "background = \"#000000\"\nforeground = \"#ffffff\"\naccent = \"#222222\"\n")
	future := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}

	if !CheckReload() {
		t.Fatal("CheckReload() should report a change after the OS theme file is rewritten with a newer mtime")
	}
	if Colors.Primary != "#222222" {
		t.Errorf("Colors.Primary = %v, want #222222", Colors.Primary)
	}
}

func TestCheckReload_NotchOverrideAppliesOnTopOfOSTheme(t *testing.T) {
	saveGlobalState(t)
	osDir, confDir := t.TempDir(), t.TempDir()

	osPath := writeOSTheme(t, osDir, "background = \"#000000\"\nforeground = \"#ffffff\"\naccent = \"#111111\"\n")
	osInfo, err := os.Stat(osPath)
	if err != nil {
		t.Fatal(err)
	}
	lastOSThemePath = osPath
	lastOSThemeMTime = osInfo.ModTime()
	haveOSThemeFile = true

	confPath := writeConfig(t, confDir, "primary = #ff00ff\n")
	confInfo, err := os.Stat(confPath)
	if err != nil {
		t.Fatal(err)
	}
	lastLoadedPath = confPath
	lastLoadedMTime = confInfo.ModTime()
	haveFile = true

	Colors = Default

	// Touch the OS theme file (unchanged content, newer mtime) to trigger a
	// reload that must re-layer the still-present notch override on top.
	future := time.Now().Add(time.Minute)
	if err := os.Chtimes(osPath, future, future); err != nil {
		t.Fatal(err)
	}

	if !CheckReload() {
		t.Fatal("CheckReload() should report a change")
	}
	if Colors.Bg != "#000000" {
		t.Errorf("Colors.Bg = %v, want #000000 (from OS theme)", Colors.Bg)
	}
	if Colors.Primary != "#ff00ff" {
		t.Errorf("Colors.Primary = %v, want #ff00ff (notch override should win over OS theme)", Colors.Primary)
	}
}

// ── Watch ────────────────────────────────────────────────────────────────────

func TestWatchDirs_ReturnsOnlyExistingDirectories(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	if got := watchDirs(); len(got) != 0 {
		t.Fatalf("watchDirs() = %v, want empty when neither directory exists", got)
	}

	notchDir := filepath.Join(tmp, "notch")
	if err := os.MkdirAll(notchDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := watchDirs(); len(got) != 1 || got[0] != notchDir {
		t.Fatalf("watchDirs() = %v, want [%v]", got, notchDir)
	}

	aliasosCurrentDir := filepath.Join(tmp, "aliasos", "current")
	if err := os.MkdirAll(aliasosCurrentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := watchDirs(); len(got) != 2 {
		t.Fatalf("watchDirs() = %v, want 2 entries", got)
	}
}

func TestWatch_ReturnsNilWhenNoWatchableDirectoryExists(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	if ch := Watch(); ch != nil {
		t.Error("Watch() should return nil when no watchable directory exists")
	}
}

func TestWatch_FiresOnOSThemeDirectorySwap(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	current := filepath.Join(tmp, "aliasos", "current")
	if err := os.MkdirAll(filepath.Join(current, "theme"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(current, "theme", "colors.toml"), []byte("background = \"#000000\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ch := Watch()
	if ch == nil {
		t.Fatal("Watch() = nil, want a channel (aliasos/current exists)")
	}

	select {
	case <-ch:
		t.Fatal("Watch() fired before any filesystem change")
	case <-time.After(200 * time.Millisecond):
	}

	// Simulate aliasos-theme-apply's actual swap mechanism: stage the new
	// theme alongside the old one, rm -rf the old, then rename the new one
	// into place — rather than editing colors.toml in place.
	next := filepath.Join(current, "next-theme")
	if err := os.MkdirAll(next, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(next, "colors.toml"), []byte("background = \"#111111\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(current, "theme")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(next, filepath.Join(current, "theme")); err != nil {
		t.Fatal(err)
	}

	select {
	case _, ok := <-ch:
		if !ok {
			t.Fatal("Watch() channel closed unexpectedly")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Watch() did not fire within 2s of the theme directory being swapped")
	}
}

func TestCheckReload_OSThemeDeletionFallsBackToDefault(t *testing.T) {
	saveGlobalState(t)
	dir := t.TempDir()
	path := writeOSTheme(t, dir, "background = \"#000000\"\nforeground = \"#ffffff\"\naccent = \"#111111\"\n")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	lastOSThemePath = path
	lastOSThemeMTime = info.ModTime()
	haveOSThemeFile = true
	Colors, _ = paletteFromOSTheme(map[string]string{"background": "#000000", "foreground": "#ffffff", "accent": "#111111"})

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	if !CheckReload() {
		t.Fatal("CheckReload() should report a change when the OS theme file is removed")
	}
	if Colors != Default {
		t.Errorf("Colors after OS theme deletion = %+v, want Default", Colors)
	}
}
