package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/aliasproject/notch/internal/db"
	"github.com/aliasproject/notch/internal/model"
	"github.com/aliasproject/notch/internal/status"
	"github.com/aliasproject/notch/internal/ui"
	"github.com/aliasproject/notch/internal/update"
	tea "github.com/charmbracelet/bubbletea"
)

// Build metadata, stamped by goreleaser via -ldflags "-X main.version=..."
// (see .goreleaser.yaml). A plain "go build" leaves them at these defaults;
// currentVersion falls back to Go's module build info in that case so a
// "go install github.com/aliasproject/notch@v0.7.1" still reports v0.7.1.
var (
	version = "dev"
	commit  = ""
	date    = ""
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-v", "--version", "version":
			fmt.Println(versionString())
			return
		case "update":
			runUpdate(os.Args[2:])
			return
		case "status":
			runStatus(os.Args[2:])
			return
		case "projects":
			runProjects(os.Args[2:])
			return
		case "clients":
			runClients(os.Args[2:])
			return
		case "tasks":
			runTasks(os.Args[2:])
			return
		case "start":
			runStart(os.Args[2:])
			return
		case "stop":
			runStop(os.Args[2:])
			return
		case "edit":
			runEdit(os.Args[2:])
			return
		case "watch":
			runWatch(os.Args[2:])
			return
		}
	}

	dbPath, err := resolveDBPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error resolving DB path: %v\n", err)
		os.Exit(1)
	}

	database, err := db.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	app, err := ui.New(database)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error initializing UI: %v\n", err)
		os.Exit(1)
	}

	p := tea.NewProgram(
		app,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error running program: %v\n", err)
		os.Exit(1)
	}
}

// runStatus prints the currently-running timer (or idle) to stdout and
// exits, for use by external status bars / shell status lines — see
// internal/status for the output formats and README.md for usage examples.
func runStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output as JSON (waybar-compatible)")
	fs.Parse(args)

	database := openDBOrExit()
	defer database.Close()

	entry, err := database.GetRunningEntry()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading running entry: %v\n", err)
		os.Exit(1)
	}

	if *jsonOut {
		fmt.Println(status.FormatJSON(entry))
	} else {
		fmt.Println(status.FormatText(entry))
	}
}

// projectOutput is the JSON shape for the "projects" subcommand, used by
// external pickers (e.g. a status-bar plugin's quick-start menu) that need
// a project ID to pass to "start" without going through the interactive UI.
// TotalSeconds is all-time accumulated duration across finished entries
// (ReportByProject excludes the currently-running one by design -- a picker
// showing a live total should add the running entry's own elapsed time on
// top of this for whichever project is active).
type projectOutput struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	ClientID     int64  `json:"clientId"`
	ClientName   string `json:"clientName"`
	TotalSeconds int64  `json:"totalSeconds"`
}

// runProjects lists all projects (across all clients) to stdout, for
// external pickers driving "start --project <id>".
func runProjects(args []string) {
	fs := flag.NewFlagSet("projects", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output as JSON")
	fs.Parse(args)

	database := openDBOrExit()
	defer database.Close()

	projects, err := database.ListProjects(0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error listing projects: %v\n", err)
		os.Exit(1)
	}

	report, err := database.ReportByProject("", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading project totals: %v\n", err)
		os.Exit(1)
	}
	totalSecondsByProject := make(map[int64]int64, len(report))
	for _, r := range report {
		totalSecondsByProject[r.ProjectID] = int64(r.TotalHours * 3600)
	}

	if *jsonOut {
		out := make([]projectOutput, len(projects))
		for i, p := range projects {
			clientName := ""
			if p.Client != nil {
				clientName = p.Client.Name
			}
			out[i] = projectOutput{ID: p.ID, Name: p.Name, ClientID: p.ClientID, ClientName: clientName, TotalSeconds: totalSecondsByProject[p.ID]}
		}
		b, err := json.Marshal(out)
		if err != nil {
			panic(err) // projectOutput is a flat struct of strings/ints -- cannot fail
		}
		fmt.Println(string(b))
		return
	}

	for _, p := range projects {
		clientName := ""
		if p.Client != nil {
			clientName = p.Client.Name
		}
		fmt.Printf("%d\t%s › %s\t%s\n", p.ID, clientName, p.Name, formatSeconds(totalSecondsByProject[p.ID]))
	}
}

// clientOutput is the JSON shape for the "clients" subcommand, used to
// drive a client picker that then narrows a project picker to that
// client's projects (see notch-plugin's Add Timer form).
type clientOutput struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// runClients lists all clients to stdout.
func runClients(args []string) {
	fs := flag.NewFlagSet("clients", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output as JSON")
	fs.Parse(args)

	database := openDBOrExit()
	defer database.Close()

	clients, err := database.ListClients()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error listing clients: %v\n", err)
		os.Exit(1)
	}

	if *jsonOut {
		out := make([]clientOutput, len(clients))
		for i, c := range clients {
			out[i] = clientOutput{ID: c.ID, Name: c.Name}
		}
		b, err := json.Marshal(out)
		if err != nil {
			panic(err) // clientOutput is a flat struct of strings/ints -- cannot fail
		}
		fmt.Println(string(b))
		return
	}

	for _, c := range clients {
		fmt.Printf("%d\t%s\n", c.ID, c.Name)
	}
}

func formatSeconds(s int64) string {
	return fmt.Sprintf("%d:%02d:%02d", s/3600, (s%3600)/60, s%60)
}

// taskOutput is the JSON shape for the "tasks" subcommand -- unlike
// "projects" (every project, no history needed), this lists specific
// previously-used tasks so a picker can resume "Meeting on Website"
// instead of just starting a bare project with no task text.
type taskOutput struct {
	ProjectID    int64  `json:"projectId"`
	ProjectName  string `json:"projectName"`
	ClientName   string `json:"clientName"`
	Task         string `json:"task"`
	TotalSeconds int64  `json:"totalSeconds"`
}

// runTasks lists the most recently-used distinct (project, task) pairings,
// for external pickers driving "start --project <id> --task <task>".
func runTasks(args []string) {
	fs := flag.NewFlagSet("tasks", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output as JSON")
	limit := fs.Int("limit", 10, "max recent tasks to list")
	fs.Parse(args)

	database := openDBOrExit()
	defer database.Close()

	tasks, err := database.ListRecentTasks(*limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error listing recent tasks: %v\n", err)
		os.Exit(1)
	}

	if *jsonOut {
		out := make([]taskOutput, len(tasks))
		for i, t := range tasks {
			out[i] = taskOutput{
				ProjectID: t.ProjectID, ProjectName: t.ProjectName,
				ClientName: t.ClientName, Task: t.Task, TotalSeconds: t.TotalSeconds,
			}
		}
		b, err := json.Marshal(out)
		if err != nil {
			panic(err) // taskOutput is a flat struct of strings/ints -- cannot fail
		}
		fmt.Println(string(b))
		return
	}

	for _, t := range tasks {
		fmt.Printf("%s\t%s › %s\t%s\n", t.Task, t.ProjectName, t.ClientName, formatSeconds(t.TotalSeconds))
	}
}

// runStart stops any currently-running entry and starts a new one, matching
// the TUI's own start semantics (see internal/ui/views/timers.go's
// submitForm/startCmd: StopAllRunning then StartEntry) so the CLI and TUI
// never disagree about "only one timer runs at a time."
//
// The project can be given either as an existing --project <id>, or by
// name via --client-name/--project-name -- the latter resolves to an
// existing client/project (case-insensitive) or creates one that doesn't
// exist yet, via db.ResolveOrCreateProject, the same find-or-create logic
// the TUI's own new-timer form uses (see that method's doc comment for the
// exact matching/creation rules: a client name with no project name uses
// the client name as the project name too, etc). If none of the three are
// given at all, falls back to db.EnsureUncategorizedProject() -- the TUI's
// own "Uncategorized / General" catch-all -- so a bare "notch start --task
// ..." (a quick-add with no client/project decided yet) always has
// somewhere valid to go, rather than erroring.
//
// --notes is deliberately CLI-only, not a TUI parity feature: the TUI's own
// "new timer" form has a Notes field, but submitForm's new-entry branch
// never applies it (StartEntry has no notes param, and unlike the edit
// branch, nothing follows up with UpdateEntry) -- typed notes are silently
// dropped there today. Here they're actually persisted, via UpdateEntry
// right after StartEntry, since there's no reason to reproduce that gap.
func runStart(args []string) {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	projectID := fs.Int64("project", 0, "existing project ID to start a timer on (see 'notch projects')")
	clientName := fs.String("client-name", "", "client name -- resolved to an existing client or created (case-insensitive match)")
	projectName := fs.String("project-name", "", "project name -- resolved to an existing project or created (case-insensitive match, scoped to the client)")
	task := fs.String("task", "", "task description")
	notes := fs.String("notes", "", "optional notes")
	fs.Parse(args)

	database := openDBOrExit()
	defer database.Close()

	var resolvedProjectID int64
	var err error
	if *projectID <= 0 && *clientName == "" && *projectName == "" {
		resolvedProjectID, err = database.EnsureUncategorizedProject()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error resolving uncategorized project: %v\n", err)
			os.Exit(1)
		}
	} else {
		resolvedProjectID, err = database.ResolveOrCreateProject(0, *clientName, *projectID, *projectName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error resolving project: %v\n", err)
			os.Exit(1)
		}
	}
	if resolvedProjectID <= 0 {
		fmt.Fprintln(os.Stderr, "error: could not resolve a project from the given flags")
		os.Exit(1)
	}

	if err := database.StopAllRunning(); err != nil {
		fmt.Fprintf(os.Stderr, "error stopping running entry: %v\n", err)
		os.Exit(1)
	}
	entry, err := database.StartEntry(resolvedProjectID, *task)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error starting entry: %v\n", err)
		os.Exit(1)
	}
	if *notes != "" {
		entry.Notes = *notes
		if err := database.UpdateEntry(entry); err != nil {
			fmt.Fprintf(os.Stderr, "error saving notes: %v\n", err)
			os.Exit(1)
		}
	}
}

// runStop stops any currently-running entry. A no-op (not an error) if
// nothing is running.
func runStop(args []string) {
	fs := flag.NewFlagSet("stop", flag.ExitOnError)
	fs.Parse(args)

	database := openDBOrExit()
	defer database.Close()

	if err := database.StopAllRunning(); err != nil {
		fmt.Fprintf(os.Stderr, "error stopping running entry: %v\n", err)
		os.Exit(1)
	}
}

// runEdit updates an existing entry's client/project/task/notes -- most
// often the currently-running one (the default target when --id is
// omitted), for turning a quick-add's placeholder Uncategorized project
// into a real one after the fact. Mirrors the TUI's own edit-entry path
// (internal/ui/views/timers.go's submitEditEntryCmd): resolve-or-create
// the project the same way (db.ResolveOrCreateProject), then apply only
// the fields actually given and UpdateEntry. An empty --task/--notes means
// "leave it as it is", not "clear it" -- there's no way to blank either
// out through this command.
func runEdit(args []string) {
	fs := flag.NewFlagSet("edit", flag.ExitOnError)
	entryID := fs.Int64("id", 0, "entry ID to edit (default: the currently-running entry)")
	clientName := fs.String("client-name", "", "client name -- resolved to an existing client or created (case-insensitive match)")
	projectName := fs.String("project-name", "", "project name -- resolved to an existing project or created (case-insensitive match, scoped to the client)")
	task := fs.String("task", "", "new task description (omit to keep the current one)")
	notes := fs.String("notes", "", "new notes (omit to keep the current ones)")
	fs.Parse(args)

	database := openDBOrExit()
	defer database.Close()

	var target *model.Entry
	if *entryID > 0 {
		entries, err := database.ListEntries(0, "", "", true)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error listing entries: %v\n", err)
			os.Exit(1)
		}
		for _, e := range entries {
			if e.ID == *entryID {
				target = e
				break
			}
		}
		if target == nil {
			fmt.Fprintf(os.Stderr, "error: entry %d not found\n", *entryID)
			os.Exit(1)
		}
	} else {
		entry, err := database.GetRunningEntry()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading running entry: %v\n", err)
			os.Exit(1)
		}
		if entry == nil {
			fmt.Fprintln(os.Stderr, "error: no timer is running -- pass --id <entry> to edit a specific one")
			os.Exit(1)
		}
		target = entry
	}

	if *clientName != "" || *projectName != "" {
		resolvedProjectID, err := database.ResolveOrCreateProject(0, *clientName, 0, *projectName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error resolving project: %v\n", err)
			os.Exit(1)
		}
		if resolvedProjectID > 0 {
			target.ProjectID = resolvedProjectID
		}
	}
	if *task != "" {
		target.Task = *task
	}
	if *notes != "" {
		target.Notes = *notes
	}

	if err := database.UpdateEntry(target); err != nil {
		fmt.Fprintf(os.Stderr, "error updating entry: %v\n", err)
		os.Exit(1)
	}
}

// runWatch runs until killed, printing a line to stdout each time an
// external process (another notch invocation, or the aliasOS/Omarchy
// bar-widget plugin) writes to the database -- see internal/db.Watch.
// Built for a consumer with no persistent SQLite connection of its own
// (the plugin: Quickshell/QML has no SQLite binding, so it can only shell
// out) that would otherwise have to poll by spawning short-lived notch CLI
// calls -- each of those touches the same WAL/shm files a real write does,
// so a bare filesystem watch can't tell a plugin's own read-only refresh
// apart from someone else's write, and ends up re-triggering itself. Run
// this once as a single long-lived background process instead: it's
// db.Watch's PRAGMA data_version check, made over this process's one
// persistent connection, that actually filters those out, the same way it
// does for the TUI's own in-process watch.
func runWatch(args []string) {
	fs := flag.NewFlagSet("watch", flag.ExitOnError)
	fs.Parse(args)

	database := openDBOrExit()
	defer database.Close()

	ch := db.Watch(database)
	if ch == nil {
		fmt.Fprintln(os.Stderr, "error: could not start filesystem watch")
		os.Exit(1)
	}

	for range ch {
		fmt.Println("changed")
	}
}

// pseudoVersion matches the versions Go synthesizes for a checkout that
// isn't at a tag, e.g. "v0.7.2-0.20260901231814-8e1a4ff10cc0+dirty". Those
// are local builds, not releases, so they're reported as "dev" rather than
// compared against GitHub.
var pseudoVersion = regexp.MustCompile(`-\d{14}-[0-9a-f]{12}|\+dirty$`)

// currentVersion is the running binary's version: the goreleaser-stamped
// one if present, else the module version Go recorded at build time (a
// real tag for "go install ...@vX.Y.Z"; "dev" for any local checkout).
func currentVersion() string {
	if version != "dev" {
		return version
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return version
	}
	if v := bi.Main.Version; v != "" && v != "(devel)" && !pseudoVersion.MatchString(v) {
		return v
	}
	return version
}

// buildDetails is the "(commit, date)" part of the version line. Stamped
// builds use goreleaser's values; local builds fall back to the VCS info Go
// embeds, marking a checkout with uncommitted changes as dirty.
func buildDetails() []string {
	var out []string
	if commit != "" {
		out = append(out, commit)
	}
	if date != "" {
		out = append(out, date)
	}
	if len(out) > 0 {
		return out
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return nil
	}
	var rev, when string
	dirty := false
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.time":
			when = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if rev != "" {
		if dirty {
			rev += "-dirty"
		}
		out = append(out, rev)
	}
	if len(when) >= 10 {
		out = append(out, when[:10])
	}
	return out
}

func versionString() string {
	v := currentVersion()
	if update.Comparable(v) && !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	s := "notch " + v
	if extra := buildDetails(); len(extra) > 0 {
		s += " (" + strings.Join(extra, ", ") + ")"
	}
	return s + " " + runtime.GOOS + "/" + runtime.GOARCH
}

// runUpdate checks GitHub for a newer release and, unless -check is given,
// downloads it and replaces the running executable in place. Local
// builds ("dev") have no version to compare against, so they only update
// with -force -- otherwise a developer's working binary would be clobbered
// by whatever happens to be the latest tag.
func runUpdate(args []string) {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	check := fs.Bool("check", false, "only report whether an update is available; don't install")
	force := fs.Bool("force", false, "install the latest release even if it isn't newer than this build")
	fs.Parse(args)

	ctx := context.Background()
	client := &update.Client{}

	rel, err := client.Latest(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error checking for updates: %v\n", err)
		os.Exit(1)
	}

	cur := currentVersion()
	switch {
	case !update.Comparable(cur):
		fmt.Printf("notch %s is a local build; latest release is %s.\n", cur, rel.Tag)
		if !*force {
			if !*check {
				fmt.Println("Run 'notch update -force' to replace it with the latest release.")
			}
			return
		}
	case update.IsNewer(cur, rel.Tag):
		fmt.Printf("notch v%s -> %s available.\n", strings.TrimPrefix(cur, "v"), rel.Tag)
	default:
		fmt.Printf("notch v%s is up to date.\n", strings.TrimPrefix(cur, "v"))
		if !*force {
			return
		}
	}
	if *check {
		return
	}

	exe, err := update.ExecutablePath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error locating executable: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Installing %s to %s...\n", rel.Tag, exe)
	if err := client.Apply(ctx, rel, runtime.GOOS, runtime.GOARCH, exe); err != nil {
		fmt.Fprintf(os.Stderr, "error installing update: %v\n", err)
		if os.IsPermission(err) || strings.Contains(err.Error(), "permission denied") {
			fmt.Fprintf(os.Stderr, "hint: %s isn't writable by you -- try 'sudo notch update'\n", filepath.Dir(exe))
		}
		os.Exit(1)
	}
	fmt.Printf("Updated notch to %s.\n", rel.Tag)
}

// openDBOrExit opens the notch database or exits the process, for
// subcommands (projects/start/stop) that follow the same
// resolve-then-open-then-exit-on-error shape runStatus already uses.
func openDBOrExit() *db.DB {
	dbPath, err := resolveDBPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error resolving DB path: %v\n", err)
		os.Exit(1)
	}
	database, err := db.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening database: %v\n", err)
		os.Exit(1)
	}
	return database
}

// resolveDBPath returns the path to the SQLite database file.
// It uses $NOTCH_DB if set, otherwise ~/.local/share/notch/notch.db
func resolveDBPath() (string, error) {
	if v := os.Getenv("NOTCH_DB"); v != "" {
		return v, nil
	}

	dataDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	dir := filepath.Join(dataDir, ".local", "share", "notch")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create data dir: %w", err)
	}

	return filepath.Join(dir, "notch.db"), nil
}
