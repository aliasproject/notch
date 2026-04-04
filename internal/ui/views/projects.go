package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/maguiard/timetui/internal/db"
	"github.com/maguiard/timetui/internal/model"
)

type projectsPane int

const (
	paneClientList projectsPane = iota
	paneProjectList
)

type projectsMode int

const (
	projectsModeList projectsMode = iota
	projectsModeNewClient
	projectsModeEditClient
	projectsModeNewProject
	projectsModeEditProject
	projectsModeConfirmDelete
)

type ProjectsModel struct {
	db      *db.DB
	width   int
	height  int
	focused projectsPane
	mode    projectsMode

	clients        []*model.Client
	projects       []*model.Project
	clientCursor   int
	projectCursor  int

	// form fields
	inputs     []textinput.Model
	focusedInput int
	deleteTarget string

	err string
}

func NewProjects(database *db.DB) ProjectsModel {
	return ProjectsModel{
		db:      database,
		focused: paneClientList,
		mode:    projectsModeList,
	}
}

// IsBusy returns true when the view is in a form or confirmation mode,
// meaning global key shortcuts should not fire.
func (m ProjectsModel) IsBusy() bool {
	return m.mode != projectsModeList
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
		if m.clientCursor >= len(m.clients) {
			m.clientCursor = maxInt(0, len(m.clients)-1)
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
	case key.Matches(msg, projectsKeys.SwitchPane):
		if m.focused == paneClientList {
			m.focused = paneProjectList
		} else {
			m.focused = paneClientList
		}

	case key.Matches(msg, projectsKeys.Up):
		if m.focused == paneClientList {
			if m.clientCursor > 0 {
				m.clientCursor--
			}
		} else {
			if m.projectCursor > 0 {
				m.projectCursor--
			}
		}

	case key.Matches(msg, projectsKeys.Down):
		if m.focused == paneClientList {
			if m.clientCursor < len(m.clients)-1 {
				m.clientCursor++
			}
		} else {
			filtered := m.filteredProjects()
			if m.projectCursor < len(filtered)-1 {
				m.projectCursor++
			}
		}

	case key.Matches(msg, projectsKeys.New):
		if m.focused == paneClientList {
			m.mode = projectsModeNewClient
			m.inputs = makeClientInputs(nil)
			m.focusedInput = 0
			m.inputs[0].Focus()
		} else {
			if len(m.clients) == 0 {
				m.err = "Create a client first"
				return m, nil
			}
			m.mode = projectsModeNewProject
			client := m.selectedClient()
			m.inputs = makeProjectInputs(nil, client)
			m.focusedInput = 0
			m.inputs[0].Focus()
		}

	case key.Matches(msg, projectsKeys.Edit):
		if m.focused == paneClientList {
			if c := m.selectedClient(); c != nil {
				m.mode = projectsModeEditClient
				m.inputs = makeClientInputs(c)
				m.focusedInput = 0
				m.inputs[0].Focus()
			}
		} else {
			if p := m.selectedProject(); p != nil {
				m.mode = projectsModeEditProject
				m.inputs = makeProjectInputs(p, nil)
				m.focusedInput = 0
				m.inputs[0].Focus()
			}
		}

	case key.Matches(msg, projectsKeys.Delete):
		if m.focused == paneClientList {
			if c := m.selectedClient(); c != nil {
				m.mode = projectsModeConfirmDelete
				m.deleteTarget = fmt.Sprintf("client \"%s\" (and all its projects/entries)", c.Name)
			}
		} else {
			if p := m.selectedProject(); p != nil {
				m.mode = projectsModeConfirmDelete
				m.deleteTarget = fmt.Sprintf("project \"%s\"", p.Name)
			}
		}
	}
	return m, nil
}

func (m ProjectsModel) updateForm(msg tea.KeyMsg) (ProjectsModel, tea.Cmd) {
	switch m.mode {
	case projectsModeConfirmDelete:
		switch msg.String() {
		case "y", "Y":
			return m.doDelete()
		case "n", "N", "esc":
			m.mode = projectsModeList
		}
		return m, nil
	}

	switch {
	case key.Matches(msg, projectsKeys.Confirm):
		return m.saveForm()

	case key.Matches(msg, projectsKeys.Cancel):
		m.mode = projectsModeList
		m.err = ""
		return m, nil

	case key.Matches(msg, projectsKeys.NextField):
		m.inputs[m.focusedInput].Blur()
		m.focusedInput = (m.focusedInput + 1) % len(m.inputs)
		m.inputs[m.focusedInput].Focus()

	case key.Matches(msg, projectsKeys.PrevField):
		m.inputs[m.focusedInput].Blur()
		m.focusedInput = (m.focusedInput - 1 + len(m.inputs)) % len(m.inputs)
		m.inputs[m.focusedInput].Focus()

	default:
		var cmd tea.Cmd
		m.inputs[m.focusedInput], cmd = m.inputs[m.focusedInput].Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m ProjectsModel) saveForm() (ProjectsModel, tea.Cmd) {
	switch m.mode {
	case projectsModeNewClient:
		name := strings.TrimSpace(m.inputs[0].Value())
		rateStr := strings.TrimSpace(m.inputs[1].Value())
		if name == "" {
			m.err = "Name is required"
			return m, nil
		}
		var rate float64
		fmt.Sscanf(rateStr, "%f", &rate)
		_, err := m.db.CreateClient(name, rate)
		if err != nil {
			m.err = err.Error()
			return m, nil
		}
		m.mode = projectsModeList
		return m, tea.Batch(loadProjectsData(m.db), StatusCmd("Client created"))

	case projectsModeEditClient:
		c := m.selectedClient()
		if c == nil {
			m.mode = projectsModeList
			return m, nil
		}
		c.Name = strings.TrimSpace(m.inputs[0].Value())
		rateStr := strings.TrimSpace(m.inputs[1].Value())
		if c.Name == "" {
			m.err = "Name is required"
			return m, nil
		}
		fmt.Sscanf(rateStr, "%f", &c.HourlyRate)
		if err := m.db.UpdateClient(c); err != nil {
			m.err = err.Error()
			return m, nil
		}
		m.mode = projectsModeList
		return m, tea.Batch(loadProjectsData(m.db), StatusCmd("Client updated"))

	case projectsModeNewProject:
		name := strings.TrimSpace(m.inputs[0].Value())
		if name == "" {
			m.err = "Name is required"
			return m, nil
		}
		client := m.selectedClient()
		if client == nil {
			m.err = "No client selected"
			return m, nil
		}
		_, err := m.db.CreateProject(client.ID, name)
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
		p.Name = strings.TrimSpace(m.inputs[0].Value())
		if p.Name == "" {
			m.err = "Name is required"
			return m, nil
		}
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
	if m.focused == paneClientList {
		if c := m.selectedClient(); c != nil {
			if err := m.db.DeleteClient(c.ID); err != nil {
				return m, ErrCmd(err.Error())
			}
			if m.clientCursor > 0 {
				m.clientCursor--
			}
			return m, tea.Batch(loadProjectsData(m.db), StatusCmd("Client deleted"))
		}
	} else {
		if p := m.selectedProject(); p != nil {
			if err := m.db.DeleteProject(p.ID); err != nil {
				return m, ErrCmd(err.Error())
			}
			if m.projectCursor > 0 {
				m.projectCursor--
			}
			return m, tea.Batch(loadProjectsData(m.db), StatusCmd("Project deleted"))
		}
	}
	return m, nil
}

// -- View ---------------------------------------------------------------------

func (m ProjectsModel) View() string {
	if m.mode != projectsModeList {
		return m.viewForm()
	}

	divW := 1
	leftW := m.width/3
	rightW := m.width - leftW - divW

	left := m.viewClientPane(leftW)
	vdiv := lipgloss.NewStyle().Foreground(cBorder).Render(
		strings.Repeat("\n│", strings.Count(left, "\n")+1),
	)
	right := m.viewProjectPane(rightW)

	panes := lipgloss.JoinHorizontal(lipgloss.Top, left, vdiv, right)

	if m.err != "" {
		return lipgloss.JoinVertical(lipgloss.Left, panes, "", errStyle.Render("✖  "+m.err))
	}
	return panes
}

func (m ProjectsModel) viewClientPane(width int) string {
	activeTitle := paneTitleStyle.Render("Clients")
	inactiveTitle := paneDimStyle.Render("Clients")

	var title string
	if m.focused == paneClientList {
		title = activeTitle
	} else {
		title = inactiveTitle
	}

	divider := paneDivStyle.Render(strings.Repeat("─", width-2))

	if len(m.clients) == 0 {
		body := mutedStyle.Render("No clients yet.  Press n to add one.")
		return lipgloss.JoinVertical(lipgloss.Left, title, divider, "", body)
	}

	rows := make([]string, len(m.clients))
	for i, c := range m.clients {
		sel := i == m.clientCursor
		prefix := RowPrefix(sel)
		name := truncate(c.Name, width-16)
		rate := mutedStyle.Padding(0, 1).Render(fmt.Sprintf("$%.0f/h", c.HourlyRate))
		var s lipgloss.Style
		if sel {
			s = selectedRowStyle
		} else {
			s = normalRowStyle
		}
		rows[i] = prefix + s.Render(name) + rate
	}

	hint := mutedStyle.Render("tab → projects  ·  n  e  d")
	return lipgloss.JoinVertical(lipgloss.Left,
		title, divider, "",
		strings.Join(rows, "\n"),
		"", hint,
	)
}

func (m ProjectsModel) viewProjectPane(width int) string {
	client := m.selectedClient()

	var titleStr string
	if client != nil {
		titleStr = paneTitleStyle.Render("Projects") +
			paneDimStyle.Render("  ·  "+client.Name)
	} else {
		if m.focused == paneProjectList {
			titleStr = paneTitleStyle.Render("Projects")
		} else {
			titleStr = paneDimStyle.Render("Projects")
		}
	}

	divider := paneDivStyle.Render(strings.Repeat("─", width-2))

	filtered := m.filteredProjects()
	if len(filtered) == 0 {
		var body string
		if client == nil {
			body = mutedStyle.Render("Select a client on the left.")
		} else {
			body = mutedStyle.Render("No projects yet.  Press n to add one.")
		}
		return lipgloss.JoinVertical(lipgloss.Left, titleStr, divider, "", body)
	}

	rows := make([]string, len(filtered))
	for i, p := range filtered {
		sel := i == m.projectCursor
		prefix := RowPrefix(sel)
		name := truncate(p.Name, width-6)
		var s lipgloss.Style
		if sel {
			s = selectedRowStyle
		} else {
			s = normalRowStyle
		}
		rows[i] = prefix + s.Render(name)
	}

	hint := mutedStyle.Render("n new  ·  e edit  ·  d del")
	return lipgloss.JoinVertical(lipgloss.Left,
		titleStr, divider, "",
		strings.Join(rows, "\n"),
		"", hint,
	)
}

func (m ProjectsModel) viewForm() string {
	if m.mode == projectsModeConfirmDelete {
		return renderConfirmDelete(m.deleteTarget, m.width)
	}

	var title string
	switch m.mode {
	case projectsModeNewClient:
		title = "New Client"
	case projectsModeEditClient:
		title = "Edit Client"
	case projectsModeNewProject:
		title = "New Project"
	case projectsModeEditProject:
		title = "Edit Project"
	}

	var fields []string
	labels := formLabels(m.mode)
	for i, inp := range m.inputs {
		focused := i == m.focusedInput
		var label string
		if focused {
			label = formLabelFocusedStyle.Render(labels[i])
		} else {
			label = formLabelStyle.Render(labels[i])
		}
		var inputStr string
		if focused {
			inputStr = formInputFocusedStyle.Render(inp.View())
		} else {
			inputStr = formInputStyle.Render(inp.View())
		}
		fields = append(fields, lipgloss.JoinHorizontal(lipgloss.Top, label, inputStr))
	}

	var errLine string
	if m.err != "" {
		errLine = "\n" + errStyle.Render("✖  "+m.err)
	}

	help := mutedStyle.Render("tab / shift+tab  move  ·  enter save  ·  esc cancel")
	body := lipgloss.JoinVertical(lipgloss.Left,
		formTitleStyle.Render(title),
		"",
		strings.Join(fields, "\n\n"),
		errLine,
		"",
		StyleButtonPrimary.Render("  Save  "),
		"",
		help,
	)

	return body
}

// -- helpers ------------------------------------------------------------------

func (m ProjectsModel) selectedClient() *model.Client {
	if len(m.clients) == 0 || m.clientCursor >= len(m.clients) {
		return nil
	}
	return m.clients[m.clientCursor]
}

func (m ProjectsModel) selectedProject() *model.Project {
	filtered := m.filteredProjects()
	if len(filtered) == 0 || m.projectCursor >= len(filtered) {
		return nil
	}
	return filtered[m.projectCursor]
}

func (m ProjectsModel) filteredProjects() []*model.Project {
	client := m.selectedClient()
	if client == nil {
		return nil
	}
	var out []*model.Project
	for _, p := range m.projects {
		if p.ClientID == client.ID {
			out = append(out, p)
		}
	}
	return out
}

func makeClientInputs(c *model.Client) []textinput.Model {
	name := textinput.New()
	name.Placeholder = "Client name"
	name.CharLimit = 64
	if c != nil {
		name.SetValue(c.Name)
	}

	rate := textinput.New()
	rate.Placeholder = "0.00"
	rate.CharLimit = 10
	if c != nil {
		rate.SetValue(fmt.Sprintf("%.2f", c.HourlyRate))
	}

	return []textinput.Model{name, rate}
}

func makeProjectInputs(p *model.Project, _ *model.Client) []textinput.Model {
	name := textinput.New()
	name.Placeholder = "Project name"
	name.CharLimit = 64
	if p != nil {
		name.SetValue(p.Name)
	}
	return []textinput.Model{name}
}

func formLabels(mode projectsMode) []string {
	switch mode {
	case projectsModeNewClient, projectsModeEditClient:
		return []string{"Name:", "Hourly Rate ($):"}
	default:
		return []string{"Name:"}
	}
}

// -- key map ------------------------------------------------------------------

type projectsKeyMap struct {
	Up          key.Binding
	Down        key.Binding
	SwitchPane  key.Binding
	New         key.Binding
	Edit        key.Binding
	Delete      key.Binding
	Confirm     key.Binding
	Cancel      key.Binding
	NextField   key.Binding
	PrevField   key.Binding
}

var projectsKeys = projectsKeyMap{
	Up:         key.NewBinding(key.WithKeys("up", "k")),
	Down:       key.NewBinding(key.WithKeys("down", "j")),
	SwitchPane: key.NewBinding(key.WithKeys("tab")),
	New:        key.NewBinding(key.WithKeys("n")),
	Edit:       key.NewBinding(key.WithKeys("e")),
	Delete:     key.NewBinding(key.WithKeys("d")),
	Confirm:    key.NewBinding(key.WithKeys("enter")),
	Cancel:     key.NewBinding(key.WithKeys("esc")),
	NextField:  key.NewBinding(key.WithKeys("tab")),
	PrevField:  key.NewBinding(key.WithKeys("shift+tab")),
}

// -- local styles -------------------------------------------------------------

var (
	paneTitleStyle        = lipgloss.NewStyle().Foreground(cText).Bold(true)
	paneDimStyle          = lipgloss.NewStyle().Foreground(cDim)
	paneDivStyle          = lipgloss.NewStyle().Foreground(cBorder)
	selectedRowStyle      = lipgloss.NewStyle().Foreground(cPrimary).Bold(true).Padding(0, 1)
	normalRowStyle        = lipgloss.NewStyle().Foreground(cText).Padding(5, 5, 5)
	mutedStyle            = lipgloss.NewStyle().Foreground(cDim)
	errStyle              = lipgloss.NewStyle().Foreground(cRed)
	formTitleStyle        = lipgloss.NewStyle().Foreground(cText).Bold(true)
	formLabelStyle        = lipgloss.NewStyle().Foreground(cDim).Width(16)
	formLabelFocusedStyle = lipgloss.NewStyle().Foreground(cAccent).Bold(true).Width(16)
	formInputStyle        = lipgloss.NewStyle().Foreground(cText).Border(lipgloss.NormalBorder(), false, false, true, false).BorderForeground(cBorder).Width(40)
	formInputFocusedStyle = lipgloss.NewStyle().Foreground(cText).Border(lipgloss.NormalBorder(), false, false, true, false).BorderForeground(cPrimary).Width(40)
)
