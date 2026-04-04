package views

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/maguiard/timetui/internal/db"
	"github.com/maguiard/timetui/internal/model"
)

// ── Modes ─────────────────────────────────────────────────────────────────────

type timersMode int

const (
	timersModeList timersMode = iota
	timersModeNew
	timersModeEdit
	timersModeConfirmDelete
)

// ── Form field indices ────────────────────────────────────────────────────────

const (
	fieldTask    = 0
	fieldClient  = 1
	fieldProject = 2
	fieldNotes   = 3
	fieldSave    = 4
	fieldCount   = 5
)

// ── Key bindings ──────────────────────────────────────────────────────────────

type timersKeyMap struct {
	Up      key.Binding
	Down    key.Binding
	New     key.Binding
	Edit    key.Binding
	Delete  key.Binding
	Toggle  key.Binding
	Invoice key.Binding
	Paid    key.Binding
	Confirm key.Binding
	Cancel  key.Binding
}

var timersKeys = timersKeyMap{
	Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	New:     key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
	Edit:    key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
	Delete:  key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
	Toggle:  key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "start/stop")),
	Invoice: key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "toggle invoice")),
	Paid:    key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "toggle paid")),
	Confirm: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
	Cancel:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
}

// ── Dropdown item ─────────────────────────────────────────────────────────────

type dropdownItem struct {
	id    int64  // 0 = "(create new)"
	label string
}

// ── Form ──────────────────────────────────────────────────────────────────────

// timerForm holds all state for the new/edit timer form.
// Client and project fields support live fuzzy filtering and inline creation.
type timerForm struct {
	entryID int64 // 0 = new

	inputs   [fieldCount]textinput.Model
	focusIdx int

	// client dropdown
	allClients      []*model.Client
	clientMatches   []dropdownItem
	clientSel       int   // index into clientMatches
	clientID        int64 // 0 = will be created from input text

	// project dropdown
	allProjects     []*model.Project
	projectMatches  []dropdownItem
	projectSel      int
	projectID       int64 // 0 = will be created from input text

	showClientDrop  bool
	showProjectDrop bool
}

func newTimerForm(clients []*model.Client, projects []*model.Project) timerForm {
	var inputs [fieldCount]textinput.Model
	inputs[fieldTask]    = makeTextInput("What are you working on?", 200, 44)
	inputs[fieldClient]  = makeTextInput("Client name (or leave blank)", 64, 44)
	inputs[fieldProject] = makeTextInput("Project name (or leave blank)", 64, 44)
	inputs[fieldNotes]   = makeTextInput("Notes (optional)", 500, 44)
	inputs[fieldTask].Focus()

	f := timerForm{
		inputs:      inputs,
		focusIdx:    fieldTask,
		allClients:  clients,
		allProjects: projects,
	}
	f.rebuildClientMatches()
	f.rebuildProjectMatches()
	return f
}

// rebuildClientMatches filters allClients by the current client input text.
func (f *timerForm) rebuildClientMatches() {
	query := strings.ToLower(strings.TrimSpace(f.inputs[fieldClient].Value()))
	f.clientMatches = nil

	for _, c := range f.allClients {
		if query == "" || strings.Contains(strings.ToLower(c.Name), query) {
			f.clientMatches = append(f.clientMatches, dropdownItem{id: c.ID, label: c.Name})
		}
	}

	// Always offer "create new" if typed text doesn't exactly match an existing name
	exactMatch := false
	for _, c := range f.allClients {
		if strings.EqualFold(c.Name, query) {
			exactMatch = true
			break
		}
	}
	if query != "" && !exactMatch {
		f.clientMatches = append([]dropdownItem{{id: 0, label: fmt.Sprintf(`+ Create "%s"`, f.inputs[fieldClient].Value())}}, f.clientMatches...)
	}

	// clamp selection
	if f.clientSel >= len(f.clientMatches) {
		f.clientSel = 0
	}
}

// rebuildProjectMatches filters allProjects by current text, scoped to selected client.
func (f *timerForm) rebuildProjectMatches() {
	query := strings.ToLower(strings.TrimSpace(f.inputs[fieldProject].Value()))
	f.projectMatches = nil

	for _, p := range f.allProjects {
		// If a client is selected, only show that client's projects
		if f.clientID != 0 && p.ClientID != f.clientID {
			continue
		}
		if query == "" || strings.Contains(strings.ToLower(p.Name), query) {
			label := p.Name
			if f.clientID == 0 && p.Client != nil {
				label = fmt.Sprintf("%s › %s", p.Client.Name, p.Name)
			}
			f.projectMatches = append(f.projectMatches, dropdownItem{id: p.ID, label: label})
		}
	}

	// Offer "create new" if typed text doesn't exactly match
	exactMatch := false
	for _, p := range f.allProjects {
		if strings.EqualFold(p.Name, query) {
			exactMatch = true
			break
		}
	}
	if query != "" && !exactMatch {
		f.projectMatches = append([]dropdownItem{{id: 0, label: fmt.Sprintf(`+ Create "%s"`, f.inputs[fieldProject].Value())}}, f.projectMatches...)
	}

	if f.projectSel >= len(f.projectMatches) {
		f.projectSel = 0
	}
}

// applyClientSelection commits the highlighted dropdown item to the client field.
func (f *timerForm) applyClientSelection() {
	if len(f.clientMatches) == 0 {
		return
	}
	item := f.clientMatches[f.clientSel]
	if item.id == 0 {
		// "create new" — keep the typed text, clear ID
		f.clientID = 0
	} else {
		f.clientID = item.id
		// Find the name and set it
		for _, c := range f.allClients {
			if c.ID == item.id {
				f.inputs[fieldClient].SetValue(c.Name)
				break
			}
		}
	}
	f.showClientDrop = false
	f.rebuildProjectMatches()
}

// applyProjectSelection commits the highlighted dropdown item to the project field.
func (f *timerForm) applyProjectSelection() {
	if len(f.projectMatches) == 0 {
		return
	}
	item := f.projectMatches[f.projectSel]
	if item.id == 0 {
		f.projectID = 0
	} else {
		f.projectID = item.id
		for _, p := range f.allProjects {
			if p.ID == item.id {
				f.inputs[fieldProject].SetValue(p.Name)
				// also fill client if blank
				if f.clientID == 0 && p.Client != nil {
					f.clientID = p.Client.ID
					f.inputs[fieldClient].SetValue(p.Client.Name)
				}
				break
			}
		}
	}
	f.showProjectDrop = false
}

// focusField blurs all inputs then focuses the given one.
func (f *timerForm) focusField(idx int) {
	for i := range f.inputs {
		f.inputs[i].Blur()
	}
	f.focusIdx = idx
	if idx < fieldSave {
		f.inputs[idx].Focus()
	}
	f.showClientDrop  = idx == fieldClient  && len(f.clientMatches) > 0
	f.showProjectDrop = idx == fieldProject && len(f.projectMatches) > 0
}

// ── Model ─────────────────────────────────────────────────────────────────────

type TimersModel struct {
	db      *db.DB
	width   int
	height  int
	mode    timersMode
	entries []*model.Entry
	cursor  int
	form    timerForm
	err     string
}

func NewTimers(database *db.DB) TimersModel {
	return TimersModel{db: database}
}

func (m *TimersModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m TimersModel) IsBusy() bool {
	return m.mode != timersModeList
}

func (m TimersModel) Init() tea.Cmd {
	return loadTimerEntriesCmd(m.db)
}

// ── Update ────────────────────────────────────────────────────────────────────

func (m TimersModel) Update(msg tea.Msg) (TimersModel, tea.Cmd) {
	switch msg := msg.(type) {

	case timerEntriesMsg:
		m.entries = []*model.Entry(msg)
		if m.cursor >= len(m.entries) && m.cursor > 0 {
			m.cursor = len(m.entries) - 1
		}
		return m, nil

	case openFormMsg:
		m = m.handleOpenForm(msg)
		return m, nil

	case tea.KeyMsg:
		switch m.mode {
		case timersModeList:
			return m.updateList(msg)
		case timersModeNew, timersModeEdit:
			return m.updateForm(msg)
		case timersModeConfirmDelete:
			return m.updateConfirm(msg)
		}
	}
	return m, nil
}

// ── List update ───────────────────────────────────────────────────────────────

func (m TimersModel) updateList(msg tea.KeyMsg) (TimersModel, tea.Cmd) {
	switch {
	case key.Matches(msg, timersKeys.Up):
		if m.cursor > 0 {
			m.cursor--
		}

	case key.Matches(msg, timersKeys.Down):
		if m.cursor < len(m.entries)-1 {
			m.cursor++
		}

	case key.Matches(msg, timersKeys.New):
		return m, m.openFormCmd(0)

	case key.Matches(msg, timersKeys.Edit):
		if len(m.entries) > 0 {
			return m, m.openFormCmd(m.entries[m.cursor].ID)
		}

	case key.Matches(msg, timersKeys.Delete):
		if len(m.entries) > 0 {
			m.mode = timersModeConfirmDelete
		}

	case key.Matches(msg, timersKeys.Toggle):
		if len(m.entries) > 0 {
			entry := m.entries[m.cursor]
			if entry.IsRunning() {
				return m, m.stopCmd(entry.ID)
			}
			return m, m.startCmd(entry.ProjectID, entry.Task)
		}

	case key.Matches(msg, timersKeys.Invoice):
		if len(m.entries) > 0 {
			entry := m.entries[m.cursor]
			if entry.IsRunning() {
				return m, ErrCmd("Stop the timer before invoicing")
			}
			return m, m.toggleInvoiceCmd(entry)
		}

	case key.Matches(msg, timersKeys.Paid):
		if len(m.entries) > 0 {
			entry := m.entries[m.cursor]
			if !entry.Invoiced {
				return m, ErrCmd("Mark as invoiced first (press i)")
			}
			return m, m.togglePaidCmd(entry)
		}
	}
	return m, nil
}

// openFormCmd loads clients+projects then opens the form.
func (m TimersModel) openFormCmd(entryID int64) tea.Cmd {
	return func() tea.Msg {
		clients, err := m.db.ListClients()
		if err != nil {
			return ErrMsg(err.Error())
		}
		projects, err := m.db.ListProjects(0)
		if err != nil {
			return ErrMsg(err.Error())
		}
		return openFormMsg{entryID: entryID, clients: clients, projects: projects}
	}
}

type openFormMsg struct {
	entryID  int64
	clients  []*model.Client
	projects []*model.Project
}

// ── Form update ───────────────────────────────────────────────────────────────

func (m TimersModel) updateForm(msg tea.KeyMsg) (TimersModel, tea.Cmd) {
	f := &m.form

	// Always allow cancel
	if key.Matches(msg, timersKeys.Cancel) {
		m.mode = timersModeList
		m.err = ""
		return m, nil
	}

	switch f.focusIdx {

	// ── Task field ────────────────────────────────────────────────────────────
	case fieldTask:
		switch {
		case msg.Type == tea.KeyTab:
			f.focusField(fieldClient)
		case msg.Type == tea.KeyShiftTab:
			f.focusField(fieldSave)
		case key.Matches(msg, timersKeys.Confirm):
			f.focusField(fieldClient)
		default:
			var cmd tea.Cmd
			f.inputs[fieldTask], cmd = f.inputs[fieldTask].Update(msg)
			return m, cmd
		}

	// ── Client field ──────────────────────────────────────────────────────────
	case fieldClient:
		switch {
		case msg.Type == tea.KeyTab:
			f.applyClientSelection()
			f.focusField(fieldProject)
		case msg.Type == tea.KeyShiftTab:
			f.applyClientSelection()
			f.focusField(fieldTask)
		case key.Matches(msg, timersKeys.Confirm):
			f.applyClientSelection()
			f.focusField(fieldProject)
		case key.Matches(msg, timersKeys.Up):
			if f.clientSel > 0 {
				f.clientSel--
			}
			f.showClientDrop = true
		case key.Matches(msg, timersKeys.Down):
			if f.clientSel < len(f.clientMatches)-1 {
				f.clientSel++
			}
			f.showClientDrop = true
		default:
			var cmd tea.Cmd
			f.inputs[fieldClient], cmd = f.inputs[fieldClient].Update(msg)
			// Reset resolved ID when user edits the text
			f.clientID = 0
			f.rebuildClientMatches()
			f.showClientDrop = len(f.clientMatches) > 0
			f.rebuildProjectMatches()
			return m, cmd
		}

	// ── Project field ─────────────────────────────────────────────────────────
	case fieldProject:
		switch {
		case msg.Type == tea.KeyTab:
			f.applyProjectSelection()
			f.focusField(fieldNotes)
		case msg.Type == tea.KeyShiftTab:
			f.applyProjectSelection()
			f.focusField(fieldClient)
		case key.Matches(msg, timersKeys.Confirm):
			f.applyProjectSelection()
			f.focusField(fieldNotes)
		case key.Matches(msg, timersKeys.Up):
			if f.projectSel > 0 {
				f.projectSel--
			}
			f.showProjectDrop = true
		case key.Matches(msg, timersKeys.Down):
			if f.projectSel < len(f.projectMatches)-1 {
				f.projectSel++
			}
			f.showProjectDrop = true
		default:
			var cmd tea.Cmd
			f.inputs[fieldProject], cmd = f.inputs[fieldProject].Update(msg)
			f.projectID = 0
			f.rebuildProjectMatches()
			f.showProjectDrop = len(f.projectMatches) > 0
			return m, cmd
		}

	// ── Notes field ───────────────────────────────────────────────────────────
	case fieldNotes:
		switch {
		case msg.Type == tea.KeyTab:
			f.focusField(fieldSave)
		case msg.Type == tea.KeyShiftTab:
			f.focusField(fieldProject)
		case key.Matches(msg, timersKeys.Confirm):
			f.focusField(fieldSave)
		default:
			var cmd tea.Cmd
			f.inputs[fieldNotes], cmd = f.inputs[fieldNotes].Update(msg)
			return m, cmd
		}

	// ── Save button ───────────────────────────────────────────────────────────
	case fieldSave:
		switch {
		case msg.Type == tea.KeyTab:
			f.focusField(fieldTask)
		case msg.Type == tea.KeyShiftTab:
			f.focusField(fieldNotes)
		case key.Matches(msg, timersKeys.Confirm):
			return m.submitForm()
		}
	}

	return m, nil
}

// ── Confirm delete ────────────────────────────────────────────────────────────

func (m TimersModel) updateConfirm(msg tea.KeyMsg) (TimersModel, tea.Cmd) {
	switch {
	case key.Matches(msg, timersKeys.Confirm):
		if len(m.entries) == 0 {
			m.mode = timersModeList
			return m, nil
		}
		return m, m.deleteCmd(m.entries[m.cursor].ID)
	case key.Matches(msg, timersKeys.Cancel):
		m.mode = timersModeList
	}
	return m, nil
}

// ── Commands ──────────────────────────────────────────────────────────────────

func (m TimersModel) stopCmd(id int64) tea.Cmd {
	return func() tea.Msg {
		if err := m.db.StopEntry(id); err != nil {
			return ErrMsg(err.Error())
		}
		return tea.BatchMsg{
			func() tea.Msg { return RunningChangedMsg{} },
			func() tea.Msg { return timerEntriesMsg(mustLoadEntries(m.db)) },
			func() tea.Msg { return StatusMsg("Timer stopped") },
		}
	}
}

func (m TimersModel) startCmd(projectID int64, task string) tea.Cmd {
	return func() tea.Msg {
		if err := m.db.StopAllRunning(); err != nil {
			return ErrMsg(err.Error())
		}
		if _, err := m.db.StartEntry(projectID, task); err != nil {
			return ErrMsg(err.Error())
		}
		return tea.BatchMsg{
			func() tea.Msg { return RunningChangedMsg{} },
			func() tea.Msg { return timerEntriesMsg(mustLoadEntries(m.db)) },
			func() tea.Msg { return StatusMsg("Timer started") },
		}
	}
}

func (m TimersModel) deleteCmd(id int64) tea.Cmd {
	return func() tea.Msg {
		if err := m.db.DeleteEntry(id); err != nil {
			return ErrMsg(err.Error())
		}
		return tea.BatchMsg{
			func() tea.Msg { return RunningChangedMsg{} },
			func() tea.Msg { return timerEntriesMsg(mustLoadEntries(m.db)) },
			func() tea.Msg { return StatusMsg("Entry deleted") },
		}
	}
}

func (m TimersModel) toggleInvoiceCmd(e *model.Entry) tea.Cmd {
	return func() tea.Msg {
		if err := m.db.SetEntryInvoiced(e.ID, !e.Invoiced); err != nil {
			return ErrMsg(err.Error())
		}
		return tea.BatchMsg{
			func() tea.Msg { return timerEntriesMsg(mustLoadEntries(m.db)) },
			func() tea.Msg { return StatusMsg("Invoice status updated") },
		}
	}
}

func (m TimersModel) togglePaidCmd(e *model.Entry) tea.Cmd {
	return func() tea.Msg {
		if err := m.db.SetEntryPaid(e.ID, !e.Paid); err != nil {
			return ErrMsg(err.Error())
		}
		return tea.BatchMsg{
			func() tea.Msg { return timerEntriesMsg(mustLoadEntries(m.db)) },
			func() tea.Msg { return StatusMsg("Payment status updated") },
		}
	}
}

// submitForm resolves/creates the client+project then starts or updates the entry.
func (m TimersModel) submitForm() (TimersModel, tea.Cmd) {
	f := m.form

	task := strings.TrimSpace(f.inputs[fieldTask].Value())
	if task == "" {
		m.err = "Task description is required"
		return m, nil
	}

	clientName  := strings.TrimSpace(f.inputs[fieldClient].Value())
	projectName := strings.TrimSpace(f.inputs[fieldProject].Value())
	notes        := strings.TrimSpace(f.inputs[fieldNotes].Value())

	clientID  := f.clientID
	projectID := f.projectID

	m.mode = timersModeList
	m.err = ""

	if f.entryID == 0 {
		// New timer
		return m, func() tea.Msg {
			resolvedProjectID, err := resolveProject(m.db, clientID, clientName, projectID, projectName)
			if err != nil {
				return ErrMsg(err.Error())
			}
			if err := m.db.StopAllRunning(); err != nil {
				return ErrMsg(err.Error())
			}
			if resolvedProjectID > 0 {
				if _, err := m.db.StartEntry(resolvedProjectID, task); err != nil {
					return ErrMsg(err.Error())
				}
			} else {
				// No project — start with a placeholder project
				pid, err := ensureUncategorizedProject(m.db)
				if err != nil {
					return ErrMsg(err.Error())
				}
				if _, err := m.db.StartEntry(pid, task); err != nil {
					return ErrMsg(err.Error())
				}
			}
			return tea.BatchMsg{
				func() tea.Msg { return RunningChangedMsg{} },
				func() tea.Msg { return timerEntriesMsg(mustLoadEntries(m.db)) },
				func() tea.Msg { return loadProjectsCmd(m.db, 0)() },
				func() tea.Msg { return loadClientsCmd(m.db)() },
				func() tea.Msg { return StatusMsg("Timer started") },
			}
		}
	}

	// Edit existing entry
	return m, func() tea.Msg {
		resolvedProjectID, err := resolveProject(m.db, clientID, clientName, projectID, projectName)
		if err != nil {
			return ErrMsg(err.Error())
		}

		entries, err := m.db.ListEntries(0, "", "", true)
		if err != nil {
			return ErrMsg(err.Error())
		}
		var target *model.Entry
		for _, e := range entries {
			if e.ID == f.entryID {
				target = e
				break
			}
		}
		if target == nil {
			return ErrMsg("Entry not found")
		}
		target.Task = task
		target.Notes = notes
		if resolvedProjectID > 0 {
			target.ProjectID = resolvedProjectID
		}
		if err := m.db.UpdateEntry(target); err != nil {
			return ErrMsg(err.Error())
		}
		return tea.BatchMsg{
			func() tea.Msg { return timerEntriesMsg(mustLoadEntries(m.db)) },
			func() tea.Msg { return loadProjectsCmd(m.db, 0)() },
			func() tea.Msg { return loadClientsCmd(m.db)() },
			func() tea.Msg { return StatusMsg("Entry updated") },
		}
	}
}

// resolveProject finds or creates the client+project and returns the project ID.
// Returns 0 if both clientName and projectName are blank (uncategorized).
func resolveProject(database *db.DB, clientID int64, clientName string, projectID int64, projectName string) (int64, error) {
	// If a project was explicitly selected from the dropdown, use it directly.
	if projectID > 0 {
		return projectID, nil
	}

	// Both blank — uncategorized
	if clientName == "" && projectName == "" {
		return 0, nil
	}

	// Resolve/create client
	if clientID == 0 && clientName != "" {
		clients, err := database.ListClients()
		if err != nil {
			return 0, err
		}
		for _, c := range clients {
			if strings.EqualFold(c.Name, clientName) {
				clientID = c.ID
				break
			}
		}
		if clientID == 0 {
			c, err := database.CreateClient(clientName, 0)
			if err != nil {
				return 0, fmt.Errorf("create client %q: %w", clientName, err)
			}
			clientID = c.ID
		}
	}

	// If only a client was specified with no project name, use client name as project too
	if projectName == "" && clientName != "" {
		projectName = clientName
	}

	// Resolve/create project
	if projectName != "" {
		projects, err := database.ListProjects(clientID)
		if err != nil {
			return 0, err
		}
		for _, p := range projects {
			if strings.EqualFold(p.Name, projectName) {
				return p.ID, nil
			}
		}
		// Create project — if no client exists yet, create one with the project name
		if clientID == 0 {
			c, err := database.CreateClient(projectName, 0)
			if err != nil {
				return 0, fmt.Errorf("create client %q: %w", projectName, err)
			}
			clientID = c.ID
		}
		p, err := database.CreateProject(clientID, projectName)
		if err != nil {
			return 0, fmt.Errorf("create project %q: %w", projectName, err)
		}
		return p.ID, nil
	}

	return 0, nil
}

// ensureUncategorizedProject returns (or creates) a catch-all project for untagged timers.
func ensureUncategorizedProject(database *db.DB) (int64, error) {
	const clientName  = "Uncategorized"
	const projectName = "General"

	clients, err := database.ListClients()
	if err != nil {
		return 0, err
	}
	var clientID int64
	for _, c := range clients {
		if c.Name == clientName {
			clientID = c.ID
			break
		}
	}
	if clientID == 0 {
		c, err := database.CreateClient(clientName, 0)
		if err != nil {
			return 0, err
		}
		clientID = c.ID
	}

	projects, err := database.ListProjects(clientID)
	if err != nil {
		return 0, err
	}
	for _, p := range projects {
		if p.Name == projectName {
			return p.ID, nil
		}
	}
	p, err := database.CreateProject(clientID, projectName)
	if err != nil {
		return 0, err
	}
	return p.ID, nil
}

func mustLoadEntries(database *db.DB) []*model.Entry {
	entries, _ := database.ListEntries(0, "", "", true)
	return entries
}

// ── Update: handle openFormMsg ────────────────────────────────────────────────

// We need to handle openFormMsg in the main Update switch.
// Re-open Update to inject it cleanly by adding a case in the top-level switch.
// This is handled below by extending Update via a patch in the Init section.

func (m TimersModel) handleOpenForm(msg openFormMsg) TimersModel {
	f := newTimerForm(msg.clients, msg.projects)
	f.entryID = msg.entryID

	// Pre-populate for edit
	if msg.entryID != 0 {
		for _, e := range m.entries {
			if e.ID == msg.entryID {
				f.inputs[fieldTask].SetValue(e.Task)
				f.inputs[fieldNotes].SetValue(e.Notes)
				if e.Project != nil {
					f.projectID = e.Project.ID
					f.inputs[fieldProject].SetValue(e.Project.Name)
					if e.Project.Client != nil {
						f.clientID = e.Project.Client.ID
						f.inputs[fieldClient].SetValue(e.Project.Client.Name)
					}
				}
				break
			}
		}
		f.rebuildClientMatches()
		f.rebuildProjectMatches()
	}

	m.form = f
	if msg.entryID == 0 {
		m.mode = timersModeNew
	} else {
		m.mode = timersModeEdit
	}
	m.err = ""
	return m
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m TimersModel) View() string {
	switch m.mode {
	case timersModeNew, timersModeEdit:
		return m.viewForm()
	case timersModeConfirmDelete:
		return m.viewConfirmDelete()
	default:
		return m.viewList()
	}
}

// ── List view ─────────────────────────────────────────────────────────────────

func (m TimersModel) viewList() string {
	if len(m.entries) == 0 {
		return m.viewEmptyState()
	}

	// Build plain-string rows. The table package applies styles via StyleFunc,
	// so content must be unstyled — no ANSI codes in the cell values.
	var rows [][]string
	for _, e := range m.entries {
		task := truncate(e.Task, 36)
		proj := ""
		if e.Project != nil {
			proj = truncate(e.Project.Client.Name+" › "+e.Project.Name, 28)
		}
		dur := model.FormatDuration(e.Duration())

		var status string
		switch {
		case e.IsRunning():
			status = "● live"
		case e.Paid:
			status = "paid"
		case e.Invoiced:
			status = "invoiced"
		default:
			status = "pending"
		}

		var earned string
		if e.Project == nil || e.Project.Client == nil || e.Project.Client.HourlyRate == 0 {
			earned = "—"
		} else {
			earned = fmt.Sprintf("$%.2f", e.Earnings(e.Project.Client.HourlyRate))
		}

		rows = append(rows, []string{status, task, proj, dur, earned})
	}

	t := table.New().
		Border(lipgloss.HiddenBorder()).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return StyleTableHeader
			}
			// In lipgloss/table v1.1.0, HeaderRow = -1 and data rows are 0-indexed.
			idx := row
			if idx < 0 || idx >= len(m.entries) {
				return StyleTableRow
			}
			entry := m.entries[idx]
			switch {
			case idx == m.cursor:
				return StyleTableRowSel
			case entry.IsRunning():
				return StyleTableRowRun
			default:
				return StyleTableRow
			}
		}).
		Headers("STATUS", "TASK", "PROJECT", "DURATION", "EARNED").
		Rows(rows...)

	// The table renders as: header line, then one line per data row.
	// HiddenBorder() produces no visible border characters between rows.
	// Line 0 = header, lines 1..N = data rows matching m.entries[0..N-1].
	rendered := t.Render()
	lines := strings.Split(strings.TrimRight(rendered, "\n"), "\n")

	var out []string
	for i, line := range lines {
		if i == 0 {
			// header row — never marked as selected
			out = append(out, RowPrefix(false)+line)
		} else {
			// data row: line index 1 maps to entry index 0
			dataIdx := i - 1
			sel := dataIdx == m.cursor
			out = append(out, RowPrefix(sel)+line)
		}
	}

	tableStr := strings.Join(out, "\n")
	detail := m.renderDetail(m.entries[m.cursor])

	return lipgloss.JoinVertical(lipgloss.Left, tableStr, "", detail)
}

func (m TimersModel) viewEmptyState() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		StyleTitle.Render("No entries yet."),
		"",
		StyleMuted.Render("Press n to start your first timer."),
		StyleMuted.Render("Client and project can be created right from the form."),
	)
}



func (m TimersModel) renderDetail(e *model.Entry) string {
	// date line
	started := e.StartTime.Local().Format("Jan 2, 2006  3:04 PM")
	dateLine := StyleMuted.Render("Started  ") + StyleTableRow.Render(started)
	if e.EndTime != nil {
		dateLine += StyleMuted.Render("  –  ") + StyleTableRow.Render(e.EndTime.Local().Format("3:04 PM"))
	}

	lines := []string{dateLine}
	if e.Notes != "" {
		lines = append(lines, StyleMuted.Render("Notes    ")+StyleTableRow.Render(e.Notes))
	}

	invoice := invoiceLabel(e)
	lines = append(lines, StyleMuted.Render("Status   ")+invoice)

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// ── Form view ─────────────────────────────────────────────────────────────────

func (m TimersModel) viewForm() string {
	f := m.form

	title := "New Timer"
	if f.entryID != 0 {
		title = "Edit Entry"
	}

	var b strings.Builder
	b.WriteString(StyleTitle.Render(title) + "\n\n")

	b.WriteString(m.renderFormField("Task", fieldTask) + "\n\n")

	b.WriteString(m.renderFormField("Client", fieldClient) + "\n")
	if f.focusIdx == fieldClient && f.showClientDrop && len(f.clientMatches) > 0 {
		b.WriteString(renderDropdown(f.clientMatches, f.clientSel) + "\n")
	}
	b.WriteString("\n")

	b.WriteString(m.renderFormField("Project", fieldProject) + "\n")
	if f.focusIdx == fieldProject && f.showProjectDrop && len(f.projectMatches) > 0 {
		b.WriteString(renderDropdown(f.projectMatches, f.projectSel) + "\n")
	}
	b.WriteString("\n")

	b.WriteString(m.renderFormField("Notes", fieldNotes) + "\n\n")

	saveBtn := StyleButtonPrimary
	if f.focusIdx == fieldSave {
		saveBtn = StyleButtonActive
	}
	b.WriteString(saveBtn.Render("  Save & Start  "))
	b.WriteString("   ")
	b.WriteString(StyleButton.Render("  Cancel  "))

	if m.err != "" {
		b.WriteString("\n\n" + StyleDanger.Render("✖  "+m.err))
	}

	b.WriteString("\n\n")
	b.WriteString(StyleHelp.Render("tab · shift+tab  move    ↑ · ↓  dropdown    enter  confirm    esc  cancel"))
	b.WriteString("\n")
	b.WriteString(StyleHelp.Render("Client and project are created automatically if they don't exist."))

	return b.String()
}

func (m TimersModel) renderFormField(label string, idx int) string {
	focused := m.form.focusIdx == idx
	ti := m.form.inputs[idx]

	var labelStr string
	if focused {
		labelStr = StyleFormLabelFocused.Render(label)
	} else {
		labelStr = StyleFormLabel.Render(label)
	}

	var inputStr string
	if focused {
		inputStr = StyleFormInputFocused.Width(48).Render(ti.View())
	} else {
		inputStr = StyleFormInput.Width(48).Render(ti.View())
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, labelStr, inputStr)
}

func renderDropdown(items []dropdownItem, sel int) string {
	start := sel - 4
	if start < 0 {
		start = 0
	}
	end := start + 7
	if end > len(items) {
		end = len(items)
	}

	var lines []string
	for i := start; i < end; i++ {
		item := items[i]
		prefix := "  "
		var s lipgloss.Style
		if i == sel {
			prefix = "▌ "
			s = StyleTableRowSel.Width(50)
		} else if item.id == 0 {
			s = StylePrimary.Width(50)
		} else {
			s = StyleMuted.Width(50)
		}
		lines = append(lines, s.Render(prefix+item.label))
	}

	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(cPrimary).
		PaddingLeft(StyleFormLabel.GetWidth()).
		Render(strings.Join(lines, "\n"))
}

func (m TimersModel) viewConfirmDelete() string {
	if len(m.entries) == 0 {
		return ""
	}
	e := m.entries[m.cursor]
	return renderConfirmDelete(fmt.Sprintf("\"%s\"", truncate(e.Task, 50)), 0)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func statusLabel(e *model.Entry) string {
	switch {
	case e.IsRunning():
		return StyleRunning.Render("● live")
	case e.Paid:
		return StyleSuccess.Render("paid")
	case e.Invoiced:
		return StyleAccent.Render("invoiced")
	default:
		return StyleMuted.Render("pending")
	}
}

func invoiceLabel(e *model.Entry) string {
	switch {
	case e.Paid:
		return StyleSuccess.Render("paid")
	case e.Invoiced:
		return StyleAccent.Render("invoiced")
	default:
		return StyleMuted.Render("pending  ") + StyleHelp.Render("i invoice · p paid")
	}
}

func billingStr(e *model.Entry) string {
	if e.Project == nil || e.Project.Client == nil || e.Project.Client.HourlyRate == 0 {
		return StyleMuted.Render("—")
	}
	return fmt.Sprintf("$%.2f", e.Earnings(e.Project.Client.HourlyRate))
}

var _ = time.Second
