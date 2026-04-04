package main

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/maguiard/timetui/internal/db"
	"github.com/maguiard/timetui/internal/ui"
)

func main() {
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

// resolveDBPath returns the path to the SQLite database file.
// It uses $TIMETUI_DB if set, otherwise ~/.local/share/timetui/timetui.db
func resolveDBPath() (string, error) {
	if v := os.Getenv("TIMETUI_DB"); v != "" {
		return v, nil
	}

	dataDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	dir := filepath.Join(dataDir, ".local", "share", "timetui")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create data dir: %w", err)
	}

	return filepath.Join(dir, "timetui.db"), nil
}
