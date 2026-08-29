# Notch.

A fast, beautiful terminal UI for tracking time — built with [Bubble Tea](https://github.com/charmbracelet/bubbletea), [Lip Gloss](https://github.com/charmbracelet/lipgloss), and a pure-Go SQLite backend.

Track time entries, manage clients and projects, set billing rates, mark work as invoiced/paid, and generate income reports — all without leaving your terminal.

---

## Features

- **Timer tracking** — start/stop timers on tasks, see live elapsed time in the header
- **Clients & Projects** — full CRUD with a two-pane browser
- **Billing rates** — set an hourly rate per client; earnings calculated automatically
- **Invoice & payment tracking** — mark entries as invoiced and/or paid
- **Reports** — aggregated hours, earnings, invoice status by project/client with quick date presets
- **Pure Go** — no CGo, no C compiler needed; cross-compiles trivially
- **Single binary** — one file, zero runtime dependencies

---

## Installation

### From source

```sh
git clone https://github.com/maguiard/timetui
cd timetui
go build -o timetui .
sudo mv timetui /usr/local/bin/
```

### Requirements

- Go 1.21+

---

## Usage

```sh
timetui
```

The database is stored at `~/.local/share/timetui/timetui.db` by default.  
Override with the `TIMETUI_DB` environment variable:

```sh
TIMETUI_DB=/path/to/custom.db timetui
```

---

## Getting Started

1. Press **3** to go to the **Clients** tab → press **n** to create a client and set a billing rate
2. Press **2** to go to the **Projects** tab → press **n** to create a project under that client
3. Press **1** to go to the **Timers** tab → press **n** to start a new timer
4. Press **space** to stop/restart a timer
5. Press **4** to view **Reports**

---

## Keybindings

### Global

| Key            | Action                 |
| -------------- | ---------------------- |
| `1`            | Switch to Timers tab   |
| `2`            | Switch to Projects tab |
| `3`            | Switch to Clients tab  |
| `4`            | Switch to Reports tab  |
| `q` / `ctrl+c` | Quit                   |

### Timers tab

| Key       | Action                                             |
| --------- | -------------------------------------------------- |
| `↑` / `k` | Move cursor up                                     |
| `↓` / `j` | Move cursor down                                   |
| `n`       | New timer (opens form, starts immediately on save) |
| `e`       | Edit selected entry                                |
| `d`       | Delete selected entry (prompts confirmation)       |
| `space`   | Start / stop selected entry's timer                |
| `i`       | Toggle invoiced status                             |
| `p`       | Toggle paid status (entry must be invoiced first)  |
| `esc`     | Cancel / back                                      |

### New / Edit timer form

| Key                 | Action                                                 |
| ------------------- | ------------------------------------------------------ |
| `tab` / `shift+tab` | Move between fields                                    |
| `↑` / `↓`           | Cycle through projects (when Project field is focused) |
| `enter`             | Confirm field / save form                              |
| `esc`               | Cancel                                                 |

### Projects tab

| Key       | Action                                                         |
| --------- | -------------------------------------------------------------- |
| `↑` / `k` | Move cursor up                                                 |
| `↓` / `j` | Move cursor down                                               |
| `tab`     | Switch focus between Clients pane and Projects pane            |
| `n`       | New client (in Clients pane) or new project (in Projects pane) |
| `e`       | Edit selected client or project                                |
| `d`       | Delete selected client or project (prompts confirmation)       |
| `esc`     | Cancel form                                                    |

### Clients tab

| Key                 | Action                                                    |
| ------------------- | --------------------------------------------------------- |
| `↑` / `k`           | Move cursor up                                            |
| `↓` / `j`           | Move cursor down                                          |
| `n`                 | New client                                                |
| `e`                 | Edit selected client (name + hourly rate)                 |
| `d`                 | Delete selected client (cascades to projects and entries) |
| `tab` / `shift+tab` | Move between form fields                                  |
| `enter`             | Save                                                      |
| `esc`               | Cancel                                                    |

### Reports tab

| Key       | Action                   |
| --------- | ------------------------ |
| `↑` / `k` | Move cursor up           |
| `↓` / `j` | Move cursor down         |
| `f`       | Open date filter         |
| `t`       | Quick filter: Today      |
| `w`       | Quick filter: This week  |
| `m`       | Quick filter: This month |
| `y`       | Quick filter: This year  |
| `a`       | Show all time            |

### Reports date filter

| Key                 | Action                          |
| ------------------- | ------------------------------- |
| `tab` / `shift+tab` | Switch between From / To fields |
| `enter`             | Apply filter                    |
| `esc`               | Cancel                          |

---

## Data

| Entity      | Fields                                                     |
| ----------- | ---------------------------------------------------------- |
| **Client**  | Name, Hourly Rate ($/hr)                                   |
| **Project** | Name, Client                                               |
| **Entry**   | Task, Project, Start time, End time, Notes, Invoiced, Paid |

Deleting a client cascades to all its projects and time entries.  
Deleting a project cascades to all its time entries.

---

## Reports

The Reports tab groups all time entries by **Client → Project** and shows:

- **Hours** — total tracked hours
- **Entries** — number of time entries
- **Invoiced** — how many entries are marked invoiced
- **Paid** — how many entries are marked paid
- **Earned** — hours × client hourly rate

Summary cards at the bottom show totals for the selected period:

- Total Hours
- Total Earned
- Outstanding (earned but not fully paid)
- Uninvoiced entries count

---

## Project Structure

```
timetui/
├── main.go                    # Entrypoint, DB init, program launch
└── internal/
    ├── model/
    │   └── model.go           # Domain types: Client, Project, Entry, ReportRow
    ├── db/
    │   └── db.go              # SQLite persistence (modernc.org/sqlite, pure Go)
    └── ui/
        ├── app.go             # Root Bubble Tea model, tab router, header/footer
        ├── styles.go          # Lip Gloss palette and shared styles
        └── views/
            ├── common.go      # Shared types, messages, styles, key maps, helpers
            ├── timers.go      # Timers tab — list, start/stop, edit, invoice/pay
            ├── projects.go    # Projects tab — two-pane client+project CRUD
            ├── clients.go     # Clients tab — client CRUD with billing rate
            └── reports.go     # Reports tab — date-filtered summary + cards
```

---

## Dependencies

| Package                              | Purpose                                 |
| ------------------------------------ | --------------------------------------- |
| `github.com/charmbracelet/bubbletea` | TUI framework (Elm architecture)        |
| `github.com/charmbracelet/bubbles`   | Ready-made components (textinput, etc.) |
| `github.com/charmbracelet/lipgloss`  | Terminal styling and layout             |
| `modernc.org/sqlite`                 | Pure-Go SQLite (no CGo)                 |

---

## License

MIT
