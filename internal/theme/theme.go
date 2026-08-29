// Package theme resolves the app's color palette. The palette tracks the
// desktop's live theme when one is available
// (~/.config/aliasos/current/theme/colors.toml, aliasos's "Aether" theme
// format), and can be further overridden by a user config file at
// $XDG_CONFIG_HOME/notch/theme.conf (falling back to
// ~/.config/notch/theme.conf) — so notch recolors itself when the desktop
// theme changes, and a notch-specific tweak always wins over both. See
// README.md for the theme.conf file format.
package theme

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/fsnotify/fsnotify"
)

// Palette holds every themable color. Field names match the theme.conf file's
// keys (lowercased, underscores for multi-word names — see fieldPtr).
type Palette struct {
	Primary   lipgloss.Color
	Accent    lipgloss.Color
	Success   lipgloss.Color
	Warning   lipgloss.Color
	Danger    lipgloss.Color
	Text      lipgloss.Color
	Dim       lipgloss.Color
	Subtle    lipgloss.Color
	Bg        lipgloss.Color
	BgAlt     lipgloss.Color
	Border    lipgloss.Color
	Highlight lipgloss.Color
}

// Default is the fallback palette: used as-is on a machine with no aliasos
// theme file, and as the base for any field an aliasos theme doesn't define.
// Matches the user's current aliasos "aether" theme (pastel blues throughout,
// dusty rose/mauve reserved for danger).
var Default = Palette{
	Primary:   "#91B0DE", // accent
	Accent:    "#9DC6E9", // cyan
	Success:   "#99C2ED", // green
	Warning:   "#A4CBF7", // yellow
	Danger:    "#C79EA9", // red
	Text:      "#C2D9E9", // light_fg
	Dim:       "#B7D2E5", // fg
	Subtle:    "#899EAC", // dark_fg
	Bg:        "#0D171F", // bg
	BgAlt:     "#252E35", // lighter_bg
	Border:    "#5F6468", // muted
	Highlight: "#AFCFFF", // bright_blue
}

// Colors is the active palette. It's resolved once here, at package-init
// time — before any importing package's own package-level vars (which copy
// out of Colors, e.g. views.cPrimary = theme.Colors.Primary) are initialized,
// per Go's guarantee that an imported package finishes initializing before
// the importing package's own initializers run.
var Colors = load()

var hexPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// lastLoadedPath/lastLoadedMTime/haveFile track the notch theme.conf state,
// and lastOSThemePath/lastOSThemeMTime/haveOSThemeFile track the aliasos
// colors.toml state, both as of the most recent successful load — so
// CheckReload can cheaply detect "nothing changed" without re-reading and
// re-parsing either file every call.
var (
	lastLoadedPath  string
	lastLoadedMTime time.Time
	haveFile        bool

	lastOSThemePath  string
	lastOSThemeMTime time.Time
	haveOSThemeFile  bool
)

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "notch", "theme.conf"), nil
}

// aliasosThemePath returns the path aliasos writes the active theme's colors
// to. aliasos rewrites this file in place whenever the user switches themes.
func aliasosThemePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "aliasos", "current", "theme", "colors.toml"), nil
}

// load resolves the initial palette: the aliasos OS theme if one is present,
// else Default, with any notch theme.conf override applied on top.
func load() Palette {
	base := Default
	if osPath, err := aliasosThemePath(); err == nil {
		lastOSThemePath = osPath
		if info, err := os.Stat(osPath); err == nil {
			lastOSThemeMTime = info.ModTime()
			haveOSThemeFile = true
			if p, ok := loadOSTheme(osPath); ok {
				base = p
			}
		}
	}

	path, err := configPath()
	if err != nil {
		return base
	}
	lastLoadedPath = path
	if info, err := os.Stat(path); err == nil {
		lastLoadedMTime = info.ModTime()
		haveFile = true
	}
	return applyOverridesTo(base, path)
}

// LoadFrom reads a notch theme.conf file at path and returns Default with
// any valid overrides applied. A missing or unreadable file returns Default
// unchanged; individual malformed lines are skipped rather than aborting the
// whole file. It does not consult the aliasos OS theme — use this to inspect
// a theme.conf file in isolation (as the tests do); the live Colors value
// goes through load/CheckReload instead, which layer OS-theme detection in
// underneath.
func LoadFrom(path string) Palette {
	return applyOverridesTo(Default, path)
}

// applyOverridesTo returns base with any valid theme.conf overrides at path
// applied on top.
func applyOverridesTo(base Palette, path string) Palette {
	p := base

	f, err := os.Open(path)
	if err != nil {
		return p
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if !hexPattern.MatchString(val) {
			continue
		}
		if ptr := p.fieldPtr(key); ptr != nil {
			*ptr = lipgloss.Color(val)
		}
	}

	return p
}

// fieldPtr returns a pointer to the palette field named by a config key, or
// nil for an unrecognized key.
func (p *Palette) fieldPtr(key string) *lipgloss.Color {
	switch key {
	case "primary":
		return &p.Primary
	case "accent":
		return &p.Accent
	case "success":
		return &p.Success
	case "warning":
		return &p.Warning
	case "danger":
		return &p.Danger
	case "text":
		return &p.Text
	case "dim":
		return &p.Dim
	case "subtle":
		return &p.Subtle
	case "bg":
		return &p.Bg
	case "bg_alt":
		return &p.BgAlt
	case "border":
		return &p.Border
	case "highlight":
		return &p.Highlight
	}
	return nil
}

// parseFlatKV reads a flat "key = value" file (# comments, blank lines
// ignored), stripping surrounding quotes from values. It's not a general
// TOML parser, but aliasos's colors.toml files are always a flat table with
// no sections or arrays, so this is sufficient.
func parseFlatKV(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	kv := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.Trim(strings.TrimSpace(val), `"`)
		kv[key] = val
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return kv, nil
}

// pickHex returns the first valid hex color found among keys, in order.
func pickHex(kv map[string]string, keys ...string) (string, bool) {
	for _, k := range keys {
		if v, ok := kv[k]; ok && hexPattern.MatchString(v) {
			return v, true
		}
	}
	return "", false
}

func hexChannels(s string) (r, g, b int, ok bool) {
	if !hexPattern.MatchString(s) {
		return 0, 0, 0, false
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return int(v >> 16 & 0xff), int(v >> 8 & 0xff), int(v & 0xff), true
}

// blend linearly interpolates from hex color a toward b by t (0=a, 1=b), for
// synthesizing a shade a theme doesn't name explicitly (e.g. an elevated
// panel background) from the two shades every theme does define.
func blend(a, b string, t float64) lipgloss.Color {
	ar, ag, ab, aok := hexChannels(a)
	br, bg, bb, bok := hexChannels(b)
	if !aok || !bok {
		return lipgloss.Color(a)
	}
	lerp := func(x, y int) int { return x + int(float64(y-x)*t) }
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", lerp(ar, br), lerp(ag, bg), lerp(ab, bb)))
}

// paletteFromOSTheme derives a Palette from a parsed aliasos colors.toml.
// Different generations of aliasos's theme generator use different key
// names for the same shade (e.g. lighter_bg vs lighter_background), and some
// minimal themes only define the base background/foreground/accent trio, so
// every field falls back through a list of known key spellings and finally
// to a value computed from background/foreground/accent — so an arbitrary
// aliasos theme always produces a complete, sensibly-ordered palette rather
// than an error.
func paletteFromOSTheme(kv map[string]string) (Palette, bool) {
	bg, ok := pickHex(kv, "background")
	if !ok {
		return Palette{}, false
	}
	fg, ok := pickHex(kv, "foreground")
	if !ok {
		return Palette{}, false
	}
	accent, ok := pickHex(kv, "accent")
	if !ok {
		accent = fg
	}

	p := Palette{
		Bg:      lipgloss.Color(bg),
		Primary: lipgloss.Color(accent),
	}

	if v, ok := pickHex(kv, "light_fg", "light_foreground", "bright_fg", "bright_foreground"); ok {
		p.Text = lipgloss.Color(v)
	} else {
		p.Text = lipgloss.Color(fg)
	}
	if v, ok := pickHex(kv, "dark_fg", "dark_foreground", "color8"); ok {
		p.Dim = lipgloss.Color(v)
	} else {
		p.Dim = blend(bg, fg, 0.55)
	}
	if v, ok := pickHex(kv, "muted"); ok {
		p.Subtle = lipgloss.Color(v)
	} else {
		p.Subtle = blend(bg, fg, 0.35)
	}
	if v, ok := pickHex(kv, "muted"); ok {
		p.Border = lipgloss.Color(v)
	} else {
		p.Border = blend(bg, fg, 0.20)
	}
	if v, ok := pickHex(kv, "lighter_bg", "lighter_background"); ok {
		p.BgAlt = lipgloss.Color(v)
	} else {
		p.BgAlt = blend(bg, fg, 0.12)
	}
	if v, ok := pickHex(kv, "green", "color2"); ok {
		p.Success = lipgloss.Color(v)
	} else {
		p.Success = p.Primary
	}
	if v, ok := pickHex(kv, "yellow", "color3"); ok {
		p.Warning = lipgloss.Color(v)
	} else {
		p.Warning = p.Primary
	}
	if v, ok := pickHex(kv, "red", "color1"); ok {
		p.Danger = lipgloss.Color(v)
	} else {
		p.Danger = p.Primary
	}
	if v, ok := pickHex(kv, "cyan", "color6"); ok {
		p.Accent = lipgloss.Color(v)
	} else {
		p.Accent = p.Primary
	}
	if v, ok := pickHex(kv, "bright_blue", "bright_cyan", "color12", "color6"); ok {
		p.Highlight = lipgloss.Color(v)
	} else {
		p.Highlight = p.Primary
	}
	return p, true
}

// loadOSTheme reads and parses the aliasos theme file at path into a
// Palette. Returns ok=false if the file is missing, unreadable, or doesn't
// define even the minimum background/foreground pair.
func loadOSTheme(path string) (Palette, bool) {
	kv, err := parseFlatKV(path)
	if err != nil {
		return Palette{}, false
	}
	return paletteFromOSTheme(kv)
}

// CheckReload re-reads the aliasos OS theme and/or the notch theme.conf
// file if either has changed since the last successful load (by mtime), or
// falls back to Default for a source that's been removed since it was last
// present. Returns true if Colors changed. Cheap to call frequently: the
// common case is one or two os.Stat calls with no reload.
func CheckReload() bool {
	changed := false

	if lastOSThemePath != "" {
		if info, err := os.Stat(lastOSThemePath); err == nil {
			if !haveOSThemeFile || info.ModTime().After(lastOSThemeMTime) {
				haveOSThemeFile = true
				lastOSThemeMTime = info.ModTime()
				changed = true
			}
		} else if haveOSThemeFile {
			haveOSThemeFile = false
			lastOSThemeMTime = time.Time{}
			changed = true
		}
	}

	if lastLoadedPath != "" {
		if info, err := os.Stat(lastLoadedPath); err == nil {
			if !haveFile || info.ModTime().After(lastLoadedMTime) {
				haveFile = true
				lastLoadedMTime = info.ModTime()
				changed = true
			}
		} else if haveFile {
			haveFile = false
			lastLoadedMTime = time.Time{}
			changed = true
		}
	}

	if !changed {
		return false
	}

	base := Default
	if haveOSThemeFile {
		if p, ok := loadOSTheme(lastOSThemePath); ok {
			base = p
		}
	}
	if haveFile {
		Colors = applyOverridesTo(base, lastLoadedPath)
	} else {
		Colors = base
	}
	return true
}

// Watch starts watching the filesystem for changes to the OS theme or the
// notch theme.conf override, and returns a channel that receives a value
// shortly after either changes — a fast trigger for CheckReload, so the UI
// doesn't have to wait for its next poll tick to notice a theme switch.
//
// It watches the *parent* directories rather than the files themselves:
// aliasos applies a new theme by rm-rf'ing and mv'ing in the whole
// current/theme directory rather than editing colors.toml in place, which
// would invalidate a watch on the file itself (inotify watches follow the
// inode, not the path — once the watched file is removed, no further events
// arrive at that path even though a new file lands there moments later).
//
// Returns nil if no watchable directory could be resolved or the OS
// watcher failed to start; callers should treat a nil channel as one that
// never fires — CheckReload's existing poll loop remains the fallback.
func Watch() <-chan struct{} {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil
	}

	dirs := watchDirs()
	if len(dirs) == 0 {
		w.Close()
		return nil
	}
	for _, d := range dirs {
		_ = w.Add(d) // best-effort: a directory that doesn't exist yet just won't fire
	}

	out := make(chan struct{}, 1)
	go func() {
		defer w.Close()
		defer close(out)

		var debounce *time.Timer
		var debounceC <-chan time.Time
		for {
			select {
			case _, ok := <-w.Events:
				if !ok {
					return
				}
				// Coalesce the burst of events a single theme switch
				// produces (directory removed, recreated, each file
				// written) into one reload check, fired 75ms after the
				// last event rather than the first.
				if debounce == nil {
					debounce = time.NewTimer(75 * time.Millisecond)
				} else {
					if !debounce.Stop() {
						select {
						case <-debounce.C:
						default:
						}
					}
					debounce.Reset(75 * time.Millisecond)
				}
				debounceC = debounce.C

			case <-debounceC:
				debounceC = nil
				select {
				case out <- struct{}{}:
				default:
				}

			case _, ok := <-w.Errors:
				if !ok {
					return
				}
			}
		}
	}()
	return out
}

// watchDirs returns the existing parent directories of the OS theme and
// notch theme.conf paths, de-duplicated.
func watchDirs() []string {
	var candidates []string
	if p, err := aliasosThemePath(); err == nil {
		// .../aliasos/current/theme/colors.toml -> .../aliasos/current
		candidates = append(candidates, filepath.Dir(filepath.Dir(p)))
	}
	if p, err := configPath(); err == nil {
		candidates = append(candidates, filepath.Dir(p))
	}

	seen := make(map[string]bool)
	var dirs []string
	for _, d := range candidates {
		if seen[d] {
			continue
		}
		seen[d] = true
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			dirs = append(dirs, d)
		}
	}
	return dirs
}
