package views

import (
	"fmt"
	"strings"

	"github.com/aliasproject/notch/internal/db"
	"github.com/aliasproject/notch/internal/model"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type projectsMode int

const (
	projectsModeList projectsMode = iota
	projectsModeNewProject
	projectsModeEditProject
	projectsModeConfirmDelete
)

// Project form field indices
const (
	projFieldClient = iota
	projFieldName
	projFieldCount
)

// listColWidths returns the PROJECT and CLIENT column widths so the list
// spans the full available content width.
func (m ProjectsModel) listColWidths() (nameCol, clientCol int) {
	avail := usableWidth(m.width) - 2
	clientCol = avail / 3
	if clientCol < 18 {
		clientCol = 18
	}
	if clientCol > 36 {
		clientCol = 36
	}
	nameCol = avail - clientCol
	if nameCol < 18 {
		nameCol = 18
	}
	return nameCol, clientCol
}

type ProjectsModel struct {
	db     *db.DB
	width  int
	height int
	mode   projectsMode

	clients  []*model.Client
	projects []*model.Project
	cursor   int

	// form fields
	inputs       []textinput.Model
	focusedInput int
	clientID     int64

	clientMatches  []dropdownItem
	clientSel      int
	showClientDrop bool

	deleteTarget string
	err          string
}

func NewProjects(database *db.DB) ProjectsModel {
	return ProjectsModel{
		db:   database,
		mode: projectsModeList,
	}
}

// IsBusy returns true when the view is in a form or confirmation mode,
// meaning global key shortcuts should not fire.
func (m ProjectsModel) IsBusy() bool {
	return m.mode != projectsModeList
}

// Help returns the contextual hotkey string for the view's current mode, for
// display in the app-level hotkey bar (see app.go renderHotkeyBar).
func (m ProjectsModel) Help() []Hotkey {
	switch m.mode {
	case projectsModeNewProject, projectsModeEditProject:
		return []Hotkey{
			{"tab / shift+tab", "move"},
			{"↑ / ↓", "dropdown"},
			{"enter", "save"},
			{"esc", "cancel"},
		}
	case projectsModeConfirmDelete:
		return []Hotkey{{"y", "confirm"}, {"esc", "cancel"}}
	default:
		return []Hotkey{{"n", "new"}, {"e", "edit"}, {"d", "delete"}}
	}
}

func (m ProjectsModel) Init() tea.Cmd {
	return loadProjectsData(m.db)
}

func (m *ProjectsModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// -- messages -----------------------------------------------------------------

type projectsDataMsg struct {
	clients  []*model.Client
	projects []*model.Project
}

func loadProjectsData(database *db.DB) tea.Cmd {
	return func() tea.Msg {
		clients, err := database.ListClients()
		if err != nil {
			return ErrMsg(err.Error())
		}
		projects, err := database.ListProjects(0)
		if err != nil {
			return ErrMsg(err.Error())
		}
		return projectsDataMsg{clients: clients, projects: projects}
	}
}

// -- Update -------------------------------------------------------------------

func (m ProjectsModel) Update(msg tea.Msg) (ProjectsModel, tea.Cmd) {
	switch msg := msg.(type) {

	case projectsDataMsg:
		m.clients = msg.clients
		m.projects = msg.projects
		if m.cursor >= len(m.projects) && m.cursor > 0 {
			m.cursor = len(m.projects) - 1
		}
		m.err = ""
		return m, nil

	case tea.KeyMsg:
		if m.mode != projectsModeList {
			return m.updateForm(msg)
		}
		return m.updateList(msg)
	}
	return m, nil
}

func (m ProjectsModel) updateList(msg tea.KeyMsg) (ProjectsModel, tea.Cmd) {
	switch {
	case key.Matches(msg, projectsKeys.Up):
		if m.cursor > 0 {
			m.cursor--
		}

	case key.Matches(msg, projectsKeys.Down):
		if m.cursor < len(m.projects)-1 {
			m.cursor++
		}

	case key.Matches(msg, projectsKeys.New):
		if len(m.clients) == 0 {
			m.err = "Create a client first (press 3)"
			return m, nil
		}
		m.mode = projectsModeNewProject
		m.inputs = makeProjectInputs(nil, "")
		m.clientID = 0
		m.focusedInput = projFieldClient
		m.inputs[projFieldClient].Focus()
		m.rebuildClientMatches()
		m.showClientDrop = len(m.clientMatches) > 0
		m.err = ""

	case key.Matches(msg, projectsKeys.Edit):
		if p := m.selectedProject(); p != nil {
			clientName := ""
			if p.Client != nil {
				clientName = p.Client.Name
				m.clientID = p.Client.ID
			}
			m.mode = projectsModeEditProject
			m.inputs = makeProjectInputs(p, clientName)
			m.focusedInput = projFieldClient
			m.inputs[projFieldClient].Focus()
			m.rebuildClientMatches()
			m.showClientDrop = false
			m.err = ""
		}

	case key.Matches(msg, projectsKeys.Delete):
		if p := m.selectedProject(); p != nil {
			m.mode = projectsModeConfirmDelete
			m.deleteTarget = fmt.Sprintf("project \"%s\"", p.Name)
		}
	}
	return m, nil
}

func (m ProjectsModel) updateForm(msg tea.KeyMsg) (ProjectsModel, tea.Cmd) {
	if m.mode == projectsModeConfirmDelete {
		switch msg.String() {
		case "y", "Y":
			return m.doDelete()
		case "n", "N", "esc":
			m.mode = projectsModeList
		}
		return m, nil
	}

	// Client dropdown navigation while the client field is focused
	if m.focusedInput == projFieldClient {
		switch {
		case msg.Type == tea.KeyUp:
			if m.clientSel > 0 {
				m.clientSel--
			}
			m.showClientDrop = true
			return m, nil
		case msg.Type == tea.KeyDown:
			if m.clientSel < len(m.clientMatches)-1 {
				m.clientSel++
			}
			m.showClientDrop = true
			return m, nil
		}
	}

	switch {
	case key.Matches(msg, projectsKeys.Confirm):
		if m.focusedInput == projFieldClient {
			m.applyClientSelection()
			m.inputs[projFieldClient].Blur()
			m.focusedInput = projFieldName
			m.inputs[projFieldName].Focus()
			m.showClientDrop = false
			return m, nil
		}
		return m.saveForm()

	case key.Matches(msg, projectsKeys.Cancel):
		m.mode = projectsModeList
		m.err = ""
		return m, nil

	case key.Matches(msg, projectsKeys.NextField):
		m.inputs[m.focusedInput].Blur()
		if m.focusedInput == projFieldClient {
			m.focusedInput = projFieldName
		} else {
			m.focusedInput = projFieldClient
		}
		m.inputs[m.focusedInput].Focus()
		m.rebuildClientMatches()
		m.showClientDrop = m.focusedInput == projFieldClient && len(m.clientMatches) > 0
		return m, nil

	case key.Matches(msg, projectsKeys.PrevField):
		m.inputs[m.focusedInput].Blur()
		if m.focusedInput == projFieldName {
			m.focusedInput = projFieldClient
		} else {
			m.focusedInput = projFieldName
		}
		m.inputs[m.focusedInput].Focus()
		m.rebuildClientMatches()
		m.showClientDrop = m.focusedInput == projFieldClient && len(m.clientMatches) > 0
		return m, nil

	default:
		var cmd tea.Cmd
		m.inputs[m.focusedInput], cmd = m.inputs[m.focusedInput].Update(msg)
		if m.focusedInput == projFieldClient {
			m.clientID = 0
			m.rebuildClientMatches()
			m.showClientDrop = len(m.clientMatches) > 0
		}
		return m, cmd
	}
}

func (m ProjectsModel) saveForm() (ProjectsModel, tea.Cmd) {
	name := strings.TrimSpace(m.inputs[projFieldName].Value())
	if name == "" {
		m.err = "Name is required"
		return m, nil
	}
	if err := m.resolveClientID(); err != nil {
		m.err = err.Error()
		return m, nil
	}

	switch m.mode {
	case projectsModeNewProject:
		_, err := m.db.CreateProject(m.clientID, name)
		if err != nil {
			m.err = err.Error()
			return m, nil
		}
		m.mode = projectsModeList
		return m, tea.Batch(loadProjectsData(m.db), StatusCmd("Project created"))

	case projectsModeEditProject:
		p := m.selectedProject()
		if p == nil {
			m.mode = projectsModeList
			return m, nil
		}
		p.ClientID = m.clientID
		p.Name = name
		if err := m.db.UpdateProject(p); err != nil {
			m.err = err.Error()
			return m, nil
		}
		m.mode = projectsModeList
		return m, tea.Batch(loadProjectsData(m.db), StatusCmd("Project updated"))
	}
	return m, nil
}

func (m ProjectsModel) doDelete() (ProjectsModel, tea.Cmd) {
	m.mode = projectsModeList
	if p := m.selectedProject(); p != nil {
		if err := m.db.DeleteProject(p.ID); err != nil {
			return m, ErrCmd(err.Error())
		}
		if m.cursor > 0 {
			m.cursor--
		}
		return m, tea.Batch(loadProjectsData(m.db), StatusCmd("Project deleted"))
	}
	return m, nil
}

// resolveClientID resolves the client from the dropdown selection or the typed text.
// It must match an existing client exactly.
func (m *ProjectsModel) resolveClientID() error {
	if m.clientID != 0 {
		return nil
	}
	text := strings.TrimSpace(m.inputs[projFieldClient].Value())
	if text == "" {
		return fmt.Errorf("Select a client")
	}
	for _, c := range m.clients {
		if strings.EqualFold(c.Name, text) {
			m.clientID = c.ID
			return nil
		}
	}
	return fmt.Errorf("Select an existing client from the dropdown")
}

// rebuildClientMatches filters clients by the current client input text.
func (m *ProjectsModel) rebuildClientMatches() {
	query := strings.ToLower(strings.TrimSpace(m.inputs[projFieldClient].Value()))
	m.clientMatches = nil
	for _, c := range m.clients {
		if query == "" || strings.Contains(strings.ToLower(c.Name), query) {
			m.clientMatches = append(m.clientMatches, dropdownItem{id: c.ID, label: c.Name})
		}
	}
	if m.clientSel >= len(m.clientMatches) {
		m.clientSel = 0
	}
}

// applyClientSelection commits the highlighted dropdown item to the client field.
func (m *ProjectsModel) applyClientSelection() {
	if len(m.clientMatches) == 0 {
		return
	}
	item := m.clientMatches[m.clientSel]
	m.clientID = item.id
	for _, c := range m.clients {
		if c.ID == item.id {
			m.inputs[projFieldClient].SetValue(c.Name)
			break
		}
	}
	m.showClientDrop = false
}

// -- View ---------------------------------------------------------------------

func (m ProjectsModel) View() string {
	if m.mode != projectsModeList {
		return m.viewForm()
	}

	var b strings.Builder

	b.WriteString(StyleTitle.Render("Projects") + "\n\n")

	if len(m.projects) == 0 {
		if len(m.clients) == 0 {
			b.WriteString(StyleMuted.Render("No clients yet.  Create one on the Clients tab (press 3)."))
		} else {
			b.WriteString(StyleMuted.Render("No projects yet.  Press n to add one."))
		}
	} else {
		nameCol, clientCol := m.listColWidths()
		b.WriteString(RowPrefix(false) +
			StyleTableHeader.Width(nameCol).Render("Project") +
			StyleTableHeader.Width(clientCol).Render("Client") + "\n")
		b.WriteString(StyleTableDiv.Render(strings.Repeat("─", usableWidth(m.width))) + "\n")

		for i, p := range m.projects {
			sel := i == m.cursor
			name := truncate(p.Name, nameCol-3)
			client := ""
			if p.Client != nil {
				client = truncate(p.Client.Name, clientCol-3)
			}

			row := RowPrefix(sel)
			if sel {
				row += StyleTableRowSel.Width(nameCol).Render(name)
				// The client column keeps its own dim foreground even when
				// selected (it's always secondary to the project name), but
				// its background still needs to follow the row's cBgAlt
				// tint — otherwise it'd leave a plain-cBg gap at the row's
				// right edge.
				row += StyleTableRowDim.Background(cBgAlt).Width(clientCol).Render(client)
			} else {
				row += StyleTableRow.Width(nameCol).Render(name)
				row += StyleTableRowDim.Width(clientCol).Render(client)
			}
			b.WriteString(row + "\n")
		}
	}

	if m.err != "" {
		b.WriteString("\n\n" + StyleDanger.Render("✖  "+m.err))
	}

	return b.String()
}

func (m ProjectsModel) viewForm() string {
	if m.mode == projectsModeConfirmDelete {
		return renderConfirmDelete(m.deleteTarget, m.width)
	}

	title := "New Project"
	if m.mode == projectsModeEditProject {
		title = "Edit Project"
	}

	var b strings.Builder
	b.WriteString(StyleTitle.Render(title) + "\n\n")

	b.WriteString(m.renderField("Client", projFieldClient) + "\n")
	if m.focusedInput == projFieldClient && m.showClientDrop && len(m.clientMatches) > 0 {
		b.WriteString(renderDropdown(m.clientMatches, m.clientSel) + "\n")
	}
	b.WriteString("\n")
	b.WriteString(m.renderField("Name", projFieldName) + "\n\n")

	b.WriteString(StyleButtonPrimary.Render("Save"))
	b.WriteString("   ")
	b.WriteString(StyleButton.Render("Cancel"))
	b.WriteString("\n\n")

	if m.err != "" {
		b.WriteString(StyleDanger.Render("✖  "+m.err) + "\n\n")
	}

	return b.String()
}

func (m ProjectsModel) renderField(label string, idx int) string {
	focused := m.focusedInput == idx
	ti := m.inputs[idx]
	refreshInputStyle(&ti)

	var labelStr string
	if focused {
		labelStr = StyleFormLabelFocused.Render(label)
	} else {
		labelStr = StyleFormLabel.Render(label)
	}

	var inputStr string
	if focused {
		inputStr = StyleFormInputFocused.Render(ti.View())
	} else {
		inputStr = StyleFormInput.Render(ti.View())
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, labelStr, inputStr)
}

// -- helpers ------------------------------------------------------------------

func (m ProjectsModel) selectedProject() *model.Project {
	if len(m.projects) == 0 || m.cursor >= len(m.projects) {
		return nil
	}
	return m.projects[m.cursor]
}

func makeProjectInputs(p *model.Project, clientName string) []textinput.Model {
	client := textinput.New()
	client.Placeholder = "Select a client"
	client.CharLimit = 64
	if clientName != "" {
		client.SetValue(clientName)
	}

	name := textinput.New()
	name.Placeholder = "Project name"
	name.CharLimit = 64
	if p != nil {
		name.SetValue(p.Name)
	}

	return []textinput.Model{client, name}
}

// -- key map ------------------------------------------------------------------

type projectsKeyMap struct {
	Up        key.Binding
	Down      key.Binding
	New       key.Binding
	Edit      key.Binding
	Delete    key.Binding
	Confirm   key.Binding
	Cancel    key.Binding
	NextField key.Binding
	PrevField key.Binding
}

var projectsKeys = projectsKeyMap{
	Up:        key.NewBinding(key.WithKeys("up", "k")),
	Down:      key.NewBinding(key.WithKeys("down", "j")),
	New:       key.NewBinding(key.WithKeys("n")),
	Edit:      key.NewBinding(key.WithKeys("e")),
	Delete:    key.NewBinding(key.WithKeys("d")),
	Confirm:   key.NewBinding(key.WithKeys("enter")),
	Cancel:    key.NewBinding(key.WithKeys("esc")),
	NextField: key.NewBinding(key.WithKeys("tab")),
	PrevField: key.NewBinding(key.WithKeys("shift+tab")),
}
