package views

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/maguiard/timetui/internal/db"
	"github.com/maguiard/timetui/internal/model"
)

// ClientsModel manages the Clients tab.
type ClientsModel struct {
	db      *db.DB
	width   int
	height  int
	clients []*model.Client
	cursor  int
	mode    clientMode

	// form fields
	inputs    []textinput.Model
	focusIdx  int
	editingID int64 // 0 = new
	formErr   string
}

type clientMode int

const (
	clientModeList clientMode = iota
	clientModeForm
	clientModeConfirmDelete
)

const (
	clientFieldName = iota
	clientFieldRate
	clientFieldCount
)

func NewClients(database *db.DB) ClientsModel {
	return ClientsModel{
		db:   database,
		mode: clientModeList,
	}
}

func (m ClientsModel) Init() tea.Cmd {
	return loadClientsCmd(m.db)
}

// IsBusy returns true when the view is in a form or confirmation mode
// and should receive all keystrokes before the root app handles them.
func (m ClientsModel) IsBusy() bool {
	return m.mode != clientModeList
}

func (m *ClientsModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// -- Update ------------------------------------------------------------------

func (m ClientsModel) Update(msg tea.Msg) (ClientsModel, tea.Cmd) {
	switch msg := msg.(type) {

	case clientsLoadedMsg:
		m.clients = msg.clients
		if m.cursor >= len(m.clients) && m.cursor > 0 {
			m.cursor = len(m.clients) - 1
		}
		return m, nil

	case tea.KeyMsg:
		switch m.mode {
		case clientModeList:
			return m.updateList(msg)
		case clientModeForm:
			return m.updateForm(msg)
		case clientModeConfirmDelete:
			return m.updateConfirm(msg)
		}
	}
	return m, nil
}

func (m ClientsModel) updateList(msg tea.KeyMsg) (ClientsModel, tea.Cmd) {
	switch {
	case key.Matches(msg, listKeys.Up):
		if m.cursor > 0 {
			m.cursor--
		}
	case key.Matches(msg, listKeys.Down):
		if m.cursor < len(m.clients)-1 {
			m.cursor++
		}
	case key.Matches(msg, listKeys.New):
		m = m.openForm(nil)
	case key.Matches(msg, listKeys.Edit):
		if len(m.clients) > 0 {
			m = m.openForm(m.clients[m.cursor])
		}
	case key.Matches(msg, listKeys.Delete):
		if len(m.clients) > 0 {
			m.mode = clientModeConfirmDelete
		}
	}
	return m, nil
}

func (m ClientsModel) updateForm(msg tea.KeyMsg) (ClientsModel, tea.Cmd) {
	switch {
	case key.Matches(msg, formKeys.Cancel):
		m.mode = clientModeList
		m.formErr = ""
		return m, nil

	case key.Matches(msg, formKeys.Submit):
		return m.submitForm()

	case key.Matches(msg, formKeys.NextField):
		m.inputs[m.focusIdx].Blur()
		m.focusIdx = (m.focusIdx + 1) % clientFieldCount
		m.inputs[m.focusIdx].Focus()
		return m, textinput.Blink

	case key.Matches(msg, formKeys.PrevField):
		m.inputs[m.focusIdx].Blur()
		m.focusIdx = (m.focusIdx - 1 + clientFieldCount) % clientFieldCount
		m.inputs[m.focusIdx].Focus()
		return m, textinput.Blink
	}

	// forward keystrokes to the focused input
	var cmd tea.Cmd
	m.inputs[m.focusIdx], cmd = m.inputs[m.focusIdx].Update(msg)
	return m, cmd
}

func (m ClientsModel) updateConfirm(msg tea.KeyMsg) (ClientsModel, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		if len(m.clients) == 0 {
			m.mode = clientModeList
			return m, nil
		}
		id := m.clients[m.cursor].ID
		if err := m.db.DeleteClient(id); err != nil {
			m.mode = clientModeList
			return m, ErrCmd(fmt.Sprintf("delete client: %v", err))
		}
		m.mode = clientModeList
		return m, tea.Batch(
			loadClientsCmd(m.db),
			loadProjectsCmd(m.db, 0),
			StatusCmd("Client deleted."),
		)
	default:
		m.mode = clientModeList
	}
	return m, nil
}

// -- Form helpers ------------------------------------------------------------

func (m ClientsModel) openForm(c *model.Client) ClientsModel {
	inputs := make([]textinput.Model, clientFieldCount)

	name := makeTextInput("Client name", 120, 36)
	rate := makeTextInput("0.00", 12, 36)

	if c != nil {
		name.SetValue(c.Name)
		rate.SetValue(fmt.Sprintf("%.2f", c.HourlyRate))
		m.editingID = c.ID
	} else {
		m.editingID = 0
	}

	inputs[clientFieldName] = name
	inputs[clientFieldRate] = rate

	m.inputs = inputs
	m.focusIdx = 0
	m.inputs[0].Focus()
	m.mode = clientModeForm
	m.formErr = ""
	return m
}

func (m ClientsModel) submitForm() (ClientsModel, tea.Cmd) {
	name := strings.TrimSpace(m.inputs[clientFieldName].Value())
	rateStr := strings.TrimSpace(m.inputs[clientFieldRate].Value())
	if rateStr == "" {
		rateStr = "0"
	}

	if name == "" {
		m.formErr = "Client name is required."
		return m, nil
	}
	rate, err := strconv.ParseFloat(rateStr, 64)
	if err != nil || rate < 0 {
		m.formErr = "Hourly rate must be a valid non-negative number."
		return m, nil
	}

	if m.editingID == 0 {
		_, err = m.db.CreateClient(name, rate)
	} else {
		err = m.db.UpdateClient(&model.Client{ID: m.editingID, Name: name, HourlyRate: rate})
	}
	if err != nil {
		m.formErr = fmt.Sprintf("Save failed: %v", err)
		return m, nil
	}

	m.mode = clientModeList
	action := "created"
	if m.editingID != 0 {
		action = "updated"
	}
	return m, tea.Batch(
		loadClientsCmd(m.db),
		loadProjectsCmd(m.db, 0),
		StatusCmd(fmt.Sprintf("Client %s.", action)),
	)
}

// -- View --------------------------------------------------------------------

func (m ClientsModel) View() string {
	switch m.mode {
	case clientModeForm:
		return m.viewForm()
	case clientModeConfirmDelete:
		return m.viewConfirm()
	default:
		return m.viewList()
	}
}

func (m ClientsModel) viewList() string {
	var sb strings.Builder

	sb.WriteString(StyleTitle.Render("Clients") + "\n\n")

	if len(m.clients) == 0 {
		sb.WriteString(StyleMuted.Render("No clients yet.") + "\n")
		sb.WriteString(StyleHelp.Render("Press n to add one."))
		return sb.String()
	}

	// header — Padding(0,1) is on StyleTableHeader so each cell gets 1 space each side
	sb.WriteString(
		RowPrefix(false) +
			StyleTableHeader.Width(30).Render("NAME") +
			StyleTableHeader.Width(18).Render("HOURLY RATE") +
			"\n",
	)
	sb.WriteString(StyleTableDiv.Render(strings.Repeat("─", 52)) + "\n")

	for i, c := range m.clients {
		sel := i == m.cursor
		prefix := RowPrefix(sel)
		name := truncate(c.Name, 27)
		rate := fmt.Sprintf("$%.2f / hr", c.HourlyRate)

		var s lipgloss.Style
		if sel {
			s = StyleTableRowSel
		} else {
			s = StyleTableRow
		}

		sb.WriteString(prefix + s.Width(30).Render(name) + s.Width(18).Render(rate) + "\n")
	}

	sb.WriteString("\n" + StyleHelp.Render("n new  ·  e edit  ·  d delete  ·  ↑/↓ navigate"))
	return sb.String()
}

func (m ClientsModel) viewForm() string {
	var sb strings.Builder

	title := "New Client"
	if m.editingID != 0 {
		title = "Edit Client"
	}
	sb.WriteString(StyleTitle.Render(title) + "\n\n")

	labels := []string{"Name", "Hourly Rate ($)"}
	for i, inp := range m.inputs {
		focused := i == m.focusIdx
		var label lipgloss.Style
		var inputStyle lipgloss.Style
		if focused {
			label = StyleFormLabelFocused
			inputStyle = StyleFormInputFocused
		} else {
			label = StyleFormLabel
			inputStyle = StyleFormInput
		}
		sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Center, label.Render(labels[i]), inputStyle.Render(inp.View())) + "\n\n")
	}

	if m.formErr != "" {
		sb.WriteString(StyleDanger.Render("✖  "+m.formErr) + "\n\n")
	}

	if m.focusIdx == clientFieldCount-1 {
		sb.WriteString(StyleButtonActive.Render("  Save  "))
	} else {
		sb.WriteString(StyleButtonPrimary.Render("  Save  "))
	}
	sb.WriteString("  " + StyleButton.Render("  Cancel  "))
	sb.WriteString("\n\n" + StyleHelp.Render("tab / shift+tab  ·  enter save  ·  esc cancel"))

	return sb.String()
}

func (m ClientsModel) viewConfirm() string {
	if len(m.clients) == 0 {
		return ""
	}
	c := m.clients[m.cursor]
	return lipgloss.JoinVertical(lipgloss.Left,
		StyleDanger.Bold(true).Render("Delete \""+c.Name+"\"?"),
		"",
		StyleMuted.Render("All projects and time entries for this client will also be deleted."),
		"",
		StyleHelp.Render("y  confirm    esc  cancel"),
	)
}

// padRight pads s with spaces to the given width (rune-aware).
func padRight(s string, width int) string {
	r := []rune(s)
	if len(r) >= width {
		return string(r[:width])
	}
	return s + strings.Repeat(" ", width-len(r))
}
