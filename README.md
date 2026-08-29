# Notch

A fast, beautiful terminal UI for tracking time — built with [Bubble Tea](https://github.com/charmbracelet/bubbletea), [Lip Gloss](https://github.com/charmbracelet/lipgloss), and a pure-Go SQLite backend.

Track time entries, manage clients and projects, set billing rates, mark work as invoiced/paid, and generate income reports — all without leaving your terminal.

---

## Features

- **Timer tracking** — start/stop timers on tasks, see the live elapsed time in the bottom status bar
- **Clients & Projects** — full CRUD, clients on their own tab with project management next door
- **Billing rates** — set an hourly rate per client; earnings calculated automatically
- **Invoice & payment tracking** — mark entries as invoiced and/or paid
- **Reports** — aggregated hours, earnings, invoice status by project/client with quick date presets
- **Pure Go** — no CGo, no C compiler needed; cross-compiles trivially
- **Single binary** — one file, zero runtime dependencies

---

## Installation

### From source

```sh
git clone https://github.com/aliasproject/notch
cd notch
go build -o notch .
sudo mv notch /usr/local/bin/
```

### Requirements

- Go 1.26+

---

## Usage

```sh
notch
```

The database is stored at `~/.local/share/notch/notch.db` by default.  
Override with the `NOTCH_DB` environment variable:

```sh
NOTCH_DB=/path/to/custom.db notch
```

---

## Status bar integration

`notch status` prints the currently-running timer (or nothing, if idle) to stdout and exits — no daemon required, it just reads the same database the TUI uses (respecting `NOTCH_DB`), so it works whether or not the TUI is currently open.

```sh
notch status         # plain text — for shell bars
notch status -json   # JSON — for waybar
```

Plain text looks like `⏱ Build feature · 1:23:45` when a timer is running, and is empty when idle.

### waybar

```jsonc
"custom/notch": {
    "exec": "notch status -json",
    "return-type": "json",
    "interval": 5
}
```

### Shell bars (tmux, i3blocks, dwm, ...)

```sh
# tmux.conf
set -g status-right '#(notch status)'
```

```sh
# i3blocks.conf
[notch]
command=notch status
interval=5
```

```sh
# dwm-style xsetroot loop
while true; do
    xsetroot -name "$(notch status)"
    sleep 5
done
```

---

## Theming

Notch's colors track your OS theme automatically, and can be further overridden by your own config file.

### Live OS theme sync

If aliasos's `~/.config/aliasos/current/theme/colors.toml` exists, Notch derives its palette from it and picks up theme switches within milliseconds (filesystem-watched, with a once-a-second poll as a fallback) — no restart needed. Different theme generations name their colors differently (some use `lighter_bg`, others `lighter_background`; minimal themes only define `background`/`foreground`/`accent`), so Notch tries several known key spellings per field and derives anything still missing, meaning any aliasos theme produces a complete, sensibly-ordered palette.

On a machine with no aliasos theme file, Notch falls back to its built-in default palette (a pastel-on-dark blue theme).

### Manual override

Create `theme.conf` in your config directory — `$XDG_CONFIG_HOME/notch/theme.conf`, or `~/.config/notch/theme.conf` if `XDG_CONFIG_HOME` isn't set — to override individual colors regardless of OS theme:

```
# theme.conf — any line omitted keeps the current (OS-synced or built-in) color
primary   = #91B0DE
accent    = #9DC6E9
success   = #99C2ED
warning   = #A4CBF7
danger    = #C79EA9
text      = #C2D9E9
dim       = #B7D2E5
subtle    = #899EAC
bg        = #0D171F
bg_alt    = #252E35
border    = #5F6468
highlight = #AFCFFF
```

- Colors must be 6-digit hex (`#RRGGBB`). Unknown keys and invalid values are ignored; anything you don't set falls back to the OS-synced color (or the built-in default shown above, on a machine with no aliasos theme).
- Lines starting with `#` are comments; blank lines are ignored.
- Priority, highest wins: `theme.conf` override → live aliasos OS theme → built-in default.
- Changes to either `theme.conf` or the OS theme are picked up automatically while Notch is running — no restart needed. Deleting `theme.conf` reverts to the OS-synced (or built-in) theme.

---

## Getting Started

1. Press **3** to go to the **Clients** tab → press **n** to create a client and set a billing rate
2. Press **2** to go to the **Projects** tab → press **n** to create a project, selecting its client from the dropdown
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

| Key       | Action                                         |
| --------- | ---------------------------------------------- |
| `↑` / `k` | Move cursor up                                 |
| `↓` / `j` | Move cursor down                               |
| `n`       | New project (pick a client from the dropdown)  |
| `e`       | Edit selected project (rename / change client) |
| `d`       | Delete selected project (prompts confirmation) |
| `esc`     | Cancel form                                    |

Clients are created and managed on the Clients tab (press **3**).

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
notch/
├── main.go                    # Entrypoint, DB init, program launch, `status` subcommand
└── internal/
    ├── model/
    │   └── model.go           # Domain types: Client, Project, Entry, ReportRow
    ├── db/
    │   └── db.go              # SQLite persistence (modernc.org/sqlite, pure Go)
    ├── theme/
    │   └── theme.go           # Color palette: OS theme sync, theme.conf overrides, live reload
    ├── status/
    │   └── status.go          # `notch status` output formatting (text/JSON)
    └── ui/
        ├── app.go             # Root Bubble Tea model, tab router, header/footer
        ├── styles.go          # Layout constants (content width, chrome rows)
        └── views/
            ├── common.go      # Shared types, messages, styles, key maps, helpers
            ├── timers.go      # Timers tab — list, start/stop, edit, invoice/pay
            ├── projects.go    # Projects tab — flat project list w/ client picker
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
| `github.com/fsnotify/fsnotify`       | Filesystem watching for live theme sync |
| `modernc.org/sqlite`                 | Pure-Go SQLite (no CGo)                 |

---

## License

MIT
