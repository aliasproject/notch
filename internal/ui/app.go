package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/maguiard/timetui/internal/db"
	"github.com/maguiard/timetui/internal/model"
	"github.com/maguiard/timetui/internal/ui/views"
)

// Tab indices
const (
	TabTimers   = 0
	TabProjects = 1
	TabClients  = 2
	TabReports  = 3
)

var tabNames = []string{"Timers", "Projects", "Clients", "Reports"}

// palette mirrors views/common.go — keeps app.go self-contained
var (
	appColorPrimary = lipgloss.Color("#7C3AED")
	appColorGreen   = lipgloss.Color("#10B981")
	appColorDim     = lipgloss.Color("#64748B")
	appColorSubtle  = lipgloss.Color("#334155")
	appColorText    = lipgloss.Color("#E2E8F0")
	appColorBg      = lipgloss.Color("#0F172A")
	appColorBgAlt   = lipgloss.Color("#1E293B")
	appColorBorder  = lipgloss.Color("#1E293B")
)

// TickMsg is sent every second to update the running timer display.
type TickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

// AppModel is the root Bubble Tea model.
type AppModel struct {
	db        *db.DB
	width     int
	height    int
	activeTab int
	running   *model.Entry
	err       string
	statusMsg string

	timers   views.TimersModel
	projects views.ProjectsModel
	clients  views.ClientsModel
	reports  views.ReportsModel
}

type appKeyMap struct {
	Tab1 key.Binding
	Tab2 key.Binding
	Tab3 key.Binding
	Tab4 key.Binding
	Quit key.Binding
}

var appKeys = appKeyMap{
	Tab1: key.NewBinding(key.WithKeys("1")),
	Tab2: key.NewBinding(key.WithKeys("2")),
	Tab3: key.NewBinding(key.WithKeys("3")),
	Tab4: key.NewBinding(key.WithKeys("4")),
	Quit: key.NewBinding(key.WithKeys("q", "ctrl+c")),
}

func New(database *db.DB) (AppModel, error) {
	running, err := database.GetRunningEntry()
	if err != nil {
		return AppModel{}, fmt.Errorf("get running entry: %w", err)
	}

	return AppModel{
		db:        database,
		activeTab: TabTimers,
		running:   running,
		timers:    views.NewTimers(database),
		projects:  views.NewProjects(database),
		clients:   views.NewClients(database),
		reports:   views.NewReports(database),
	}, nil
}

func (m AppModel) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(),
		m.timers.Init(),
		m.projects.Init(),
		m.clients.Init(),
		m.reports.Init(),
	)
}

func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		cw, ch := m.contentDims()
		m.timers.SetSize(cw, ch)
		m.projects.SetSize(cw, ch)
		m.clients.SetSize(cw, ch)
		m.reports.SetSize(cw, ch)
		return m, nil

	case TickMsg:
		cmds = append(cmds, tickCmd())
		m.running, _ = m.db.GetRunningEntry()

	case tea.KeyMsg:
		if !m.activeViewBusy() {
			switch {
			case key.Matches(msg, appKeys.Quit):
				return m, tea.Quit
			case key.Matches(msg, appKeys.Tab1):
				m.activeTab = TabTimers
				return m, nil
			case key.Matches(msg, appKeys.Tab2):
				m.activeTab = TabProjects
				return m, nil
			case key.Matches(msg, appKeys.Tab3):
				m.activeTab = TabClients
				return m, nil
			case key.Matches(msg, appKeys.Tab4):
				m.activeTab = TabReports
				return m, nil
			}
		}
		if key.Matches(msg, appKeys.Quit) {
			return m, tea.Quit
		}

	case views.StatusMsg:
		m.statusMsg = string(msg)
		m.err = ""
		return m, nil

	case views.ErrMsg:
		m.err = string(msg)
		m.statusMsg = ""
		return m, nil

	case views.RunningChangedMsg:
		m.running, _ = m.db.GetRunningEntry()
	}

	// Key events → active tab only. Everything else → all tabs.
	if _, isKey := msg.(tea.KeyMsg); isKey {
		switch m.activeTab {
		case TabTimers:
			u, c := m.timers.Update(msg)
			m.timers = u
			cmds = append(cmds, c)
		case TabProjects:
			u, c := m.projects.Update(msg)
			m.projects = u
			cmds = append(cmds, c)
		case TabClients:
			u, c := m.clients.Update(msg)
			m.clients = u
			cmds = append(cmds, c)
		case TabReports:
			u, c := m.reports.Update(msg)
			m.reports = u
			cmds = append(cmds, c)
		}
	} else {
		u1, c1 := m.timers.Update(msg)
		m.timers = u1
		cmds = append(cmds, c1)

		u2, c2 := m.projects.Update(msg)
		m.projects = u2
		cmds = append(cmds, c2)

		u3, c3 := m.clients.Update(msg)
		m.clients = u3
		cmds = append(cmds, c3)

		u4, c4 := m.reports.Update(msg)
		m.reports = u4
		cmds = append(cmds, c4)
	}

	return m, tea.Batch(cmds...)
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m AppModel) View() string {
	if m.width == 0 {
		return ""
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		m.renderTopBar(),
		m.renderBody(),
		m.renderFooter(),
	)
}

// renderTopBar renders a full-width bar with: app name | tabs centered | timer.
// Keep it simple: build a plain string, then render it once in a styled container.
func (m AppModel) renderTopBar() string {
	// ── app name (left) ───────────────────────────────────────────────────────
	appName := lipgloss.NewStyle().
		Foreground(appColorPrimary).
		Bold(true).
		Padding(0, 1).
		Render("⏱  timetui")

	// ── tabs (center) ─────────────────────────────────────────────────────────
	activeSty := lipgloss.NewStyle().
		Foreground(appColorText).
		Bold(true).
		Padding(0, 2)
	inactiveSty := lipgloss.NewStyle().
		Foreground(appColorDim).
		Padding(0, 2)
	sepSty := lipgloss.NewStyle().Foreground(appColorSubtle)

	var tabParts []string
	for i, name := range tabNames {
		if i == m.activeTab {
			tabParts = append(tabParts, activeSty.Render(name))
		} else {
			tabParts = append(tabParts, inactiveSty.Render(name))
		}
	}
	tabRow := strings.Join(tabParts, sepSty.Render(" │ "))

	// ── timer (right) ─────────────────────────────────────────────────────────
	timerStr := m.renderTimerStr()

	// ── layout: pad so tabs are centred, timer is flush right ─────────────────
	appW   := lipgloss.Width(appName)
	tabW   := lipgloss.Width(tabRow)
	timerW := lipgloss.Width(timerStr)

	// Space available after the three fixed sections
	space := m.width - appW - tabW - timerW
	if space < 2 {
		space = 2
	}
	leftGap  := strings.Repeat(" ", space/2)
	rightGap := strings.Repeat(" ", space-space/2)

	line := appName + leftGap + tabRow + rightGap + timerStr

	// Render the whole line once inside the bar container (padding adds top/bottom rows)
	return lipgloss.NewStyle().
		Background(appColorBgAlt).
		Foreground(appColorText).
		Width(m.width).
		Padding(1, 0).
		Render(line)
}

// renderTimerStr returns a plain styled string for the timer chip — no outer padding.
func (m AppModel) renderTimerStr() string {
	if m.running == nil {
		return lipgloss.NewStyle().
			Foreground(appColorSubtle).
			Padding(0, 1).
			Render("● idle")
	}

	dur  := model.FormatDuration(m.running.Duration())
	task := truncateStr(m.running.Task, 28)

	chip := lipgloss.NewStyle().Foreground(appColorGreen).Bold(true).Render("● "+dur) +
		"  " +
		lipgloss.NewStyle().Foreground(appColorText).Render(task)

	if m.running.Project != nil {
		p := m.running.Project.Client.Name + " › " + m.running.Project.Name
		chip += "  " + lipgloss.NewStyle().Foreground(appColorDim).Render(truncateStr(p, 28))
	}

	return chip
}

func truncateStr(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

// renderBody renders the centered content column.
func (m AppModel) renderBody() string {
	_, ch := m.contentDims()
	cw := ContentWidth(m.width)
	pad := ContentPad(m.width)

	var view string
	switch m.activeTab {
	case TabTimers:
		view = m.timers.View()
	case TabProjects:
		view = m.projects.View()
	case TabClients:
		view = m.clients.View()
	case TabReports:
		view = m.reports.View()
	}

	// Content column — no background set so terminal default shows through cleanly
	column := lipgloss.NewStyle().
		Width(cw).
		Height(ch).
		Background(appColorBg).
		Padding(1, 3).
		Render(view)

	if pad == 0 {
		return column
	}

	// Gutters match the body background — no color blocks
	gutter := lipgloss.NewStyle().
		Background(appColorBg).
		Width(pad).
		Height(ch).
		Render("")

	return lipgloss.JoinHorizontal(lipgloss.Top, gutter, column, gutter)
}

// renderFooter renders a minimal single-line status/help bar.
func (m AppModel) renderFooter() string {
	var center string
	switch {
	case m.err != "":
		center = lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Bold(true).Render("✖  " + m.err)
	case m.statusMsg != "":
		center = lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981")).Bold(true).Render("✔  " + m.statusMsg)
	default:
		if m.activeViewBusy() {
			center = lipgloss.NewStyle().Foreground(appColorSubtle).Render("enter confirm  ·  esc cancel  ·  tab next field")
		} else {
			center = lipgloss.NewStyle().Foreground(appColorSubtle).Render("1-4 tabs  ·  n new  ·  e edit  ·  d delete  ·  space start/stop  ·  q quit")
		}
	}

	return lipgloss.NewStyle().
		Background(appColorBgAlt).
		Foreground(appColorDim).
		Width(m.width).
		Align(lipgloss.Center).
		Padding(0, 2).
		Render(center)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func (m AppModel) activeViewBusy() bool {
	switch m.activeTab {
	case TabTimers:
		return m.timers.IsBusy()
	case TabProjects:
		return m.projects.IsBusy()
	case TabClients:
		return m.clients.IsBusy()
	case TabReports:
		return m.reports.IsBusy()
	}
	return false
}

// contentDims returns the width and height of the centered content column.
// The top bar is now 3 rows tall (1 pad + content + 1 pad) and the footer
// is 1 row, so ChromeRows = 4. This is set in styles.go.
func (m AppModel) contentDims() (width, height int) {
	return ContentWidth(m.width), m.height - ChromeRows
}
