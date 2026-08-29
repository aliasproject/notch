package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aliasproject/notch/internal/db"
	"github.com/aliasproject/notch/internal/status"
	"github.com/aliasproject/notch/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "status" {
		runStatus(os.Args[2:])
		return
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
