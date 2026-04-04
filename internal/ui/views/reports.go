package views

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/maguiard/timetui/internal/db"
	"github.com/maguiard/timetui/internal/model"
)

// ── Column widths ─────────────────────────────────────────────────────────────

const (
	rColGutter  = 2
	rColClient  = 22
	rColProject = 26
	rColHours   = 12
	rColEntries = 12
	rColInv     = 12
	rColPaid    = 12
	rColEarned  = 14
)

type reportsMode int

const (
	reportsModeView reportsMode = iota
	reportsModeFilter
)

type ReportsModel struct {
	db     *db.DB
	width  int
	height int
	mode   reportsMode

	rows   []*model.ReportRow
	cursor int

	fromInput textinput.Model
	toInput   textinput.Model
	filterIdx int

	dateFrom string
	dateTo   string
	err      string
}

func NewReports(database *db.DB) ReportsModel {
	from := textinput.New()
	from.Placeholder = "YYYY-MM-DD"
	from.CharLimit = 10
	from.Width = 14

	to := textinput.New()
	to.Placeholder = "YYYY-MM-DD"
	to.CharLimit = 10
	to.Width = 14

	// Default: current month
	now := time.Now()
	defaultFrom := fmt.Sprintf("%d-%02d-01", now.Year(), now.Month())
	defaultTo := now.Format("2006-01-02")
	from.SetValue(defaultFrom)
	to.SetValue(defaultTo)

	return ReportsModel{
		db:        database,
		fromInput: from,
		toInput:   to,
		dateFrom:  defaultFrom,
		dateTo:    defaultTo,
	}
}

func (m ReportsModel) Init() tea.Cmd {
	return m.loadCmd()
}

// IsBusy returns true when the view is in a mode that should capture all keystrokes.
func (m ReportsModel) IsBusy() bool {
	return m.mode == reportsModeFilter
}

func (m *ReportsModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// -- messages -----------------------------------------------------------------

type reportRowsMsg []*model.ReportRow

func (m ReportsModel) loadCmd() tea.Cmd {
	from := m.dateFrom
	to := m.dateTo
	return func() tea.Msg {
		rows, err := m.db.ReportByProject(from, to)
		if err != nil {
			return ErrMsg(err.Error())
		}
		return reportRowsMsg(rows)
	}
}

// -- Update -------------------------------------------------------------------

type reportsKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Filter key.Binding
	Apply  key.Binding
	Cancel key.Binding
	Today  key.Binding
	Week   key.Binding
	Month  key.Binding
	Year   key.Binding
	All    key.Binding
	Next   key.Binding
	Prev   key.Binding
}

var reportsKeys = reportsKeyMap{
	Up:     key.NewBinding(key.WithKeys("up", "k")),
	Down:   key.NewBinding(key.WithKeys("down", "j")),
	Filter: key.NewBinding(key.WithKeys("f")),
	Apply:  key.NewBinding(key.WithKeys("enter")),
	Cancel: key.NewBinding(key.WithKeys("esc")),
	Today:  key.NewBinding(key.WithKeys("t")),
	Week:   key.NewBinding(key.WithKeys("w")),
	Month:  key.NewBinding(key.WithKeys("m")),
	Year:   key.NewBinding(key.WithKeys("y")),
	All:    key.NewBinding(key.WithKeys("a")),
	Next:   key.NewBinding(key.WithKeys("tab")),
	Prev:   key.NewBinding(key.WithKeys("shift+tab")),
}

func (m ReportsModel) Update(msg tea.Msg) (ReportsModel, tea.Cmd) {
	switch msg := msg.(type) {

	case reportRowsMsg:
		m.rows = []*model.ReportRow(msg)
		if m.cursor >= len(m.rows) && m.cursor > 0 {
			m.cursor = len(m.rows) - 1
		}
		m.err = ""
		return m, nil

	case tea.KeyMsg:
		if m.mode == reportsModeFilter {
			return m.updateFilter(msg)
		}
		return m.updateView(msg)
	}
	return m, nil
}

func (m ReportsModel) updateView(msg tea.KeyMsg) (ReportsModel, tea.Cmd) {
	switch {
	case key.Matches(msg, reportsKeys.Up):
		if m.cursor > 0 {
			m.cursor--
		}
	case key.Matches(msg, reportsKeys.Down):
		if m.cursor < len(m.rows)-1 {
			m.cursor++
		}
	case key.Matches(msg, reportsKeys.Filter):
		m.mode = reportsModeFilter
		m.filterIdx = 0
		m.fromInput.Focus()
		m.toInput.Blur()
		return m, textinput.Blink
	case key.Matches(msg, reportsKeys.Today):
		today := time.Now().Format("2006-01-02")
		m.dateFrom = today
		m.dateTo = today
		m.fromInput.SetValue(today)
		m.toInput.SetValue(today)
		return m, m.loadCmd()
	case key.Matches(msg, reportsKeys.Week):
		now := time.Now()
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		monday := now.AddDate(0, 0, -(weekday - 1))
		m.dateFrom = monday.Format("2006-01-02")
		m.dateTo = now.Format("2006-01-02")
		m.fromInput.SetValue(m.dateFrom)
		m.toInput.SetValue(m.dateTo)
		return m, m.loadCmd()
	case key.Matches(msg, reportsKeys.Month):
		now := time.Now()
		m.dateFrom = fmt.Sprintf("%d-%02d-01", now.Year(), now.Month())
		m.dateTo = now.Format("2006-01-02")
		m.fromInput.SetValue(m.dateFrom)
		m.toInput.SetValue(m.dateTo)
		return m, m.loadCmd()
	case key.Matches(msg, reportsKeys.Year):
		now := time.Now()
		m.dateFrom = fmt.Sprintf("%d-01-01", now.Year())
		m.dateTo = now.Format("2006-01-02")
		m.fromInput.SetValue(m.dateFrom)
		m.toInput.SetValue(m.dateTo)
		return m, m.loadCmd()
	case key.Matches(msg, reportsKeys.All):
		m.dateFrom = ""
		m.dateTo = ""
		m.fromInput.SetValue("")
		m.toInput.SetValue("")
		return m, m.loadCmd()
	}
	return m, nil
}

func (m ReportsModel) updateFilter(msg tea.KeyMsg) (ReportsModel, tea.Cmd) {
	switch {
	case key.Matches(msg, reportsKeys.Cancel):
		m.mode = reportsModeView
		m.fromInput.Blur()
		m.toInput.Blur()
		return m, nil

	case key.Matches(msg, reportsKeys.Apply):
		m.dateFrom = strings.TrimSpace(m.fromInput.Value())
		m.dateTo = strings.TrimSpace(m.toInput.Value())
		m.mode = reportsModeView
		m.fromInput.Blur()
		m.toInput.Blur()
		return m, m.loadCmd()

	case key.Matches(msg, reportsKeys.Next):
		m.fromInput.Blur()
		m.toInput.Blur()
		m.filterIdx = (m.filterIdx + 1) % 2
		if m.filterIdx == 0 {
			m.fromInput.Focus()
		} else {
			m.toInput.Focus()
		}
		return m, textinput.Blink

	case key.Matches(msg, reportsKeys.Prev):
		m.fromInput.Blur()
		m.toInput.Blur()
		m.filterIdx = (m.filterIdx - 1 + 2) % 2
		if m.filterIdx == 0 {
			m.fromInput.Focus()
		} else {
			m.toInput.Focus()
		}
		return m, textinput.Blink
	}

	// Forward to focused input
	var cmd tea.Cmd
	if m.filterIdx == 0 {
		m.fromInput, cmd = m.fromInput.Update(msg)
	} else {
		m.toInput, cmd = m.toInput.Update(msg)
	}
	return m, cmd
}

// -- View ---------------------------------------------------------------------

func (m ReportsModel) View() string {
	var sb strings.Builder

	sb.WriteString(m.renderHeader())

	if m.mode == reportsModeFilter {
		sb.WriteString("\n\n")
		sb.WriteString(m.renderFilterBar())
	}

	sb.WriteString("\n\n")

	if len(m.rows) == 0 {
		sb.WriteString(StyleMuted.Render("No data for the selected period."))
		sb.WriteString("\n\n")
		sb.WriteString(m.renderQuickHelp())
		return sb.String()
	}

	sb.WriteString(m.renderTable())
	sb.WriteString("\n")

	// Drill-down: show tasks for the selected project row
	if m.cursor < len(m.rows) {
		sb.WriteString(m.renderEntries(m.rows[m.cursor]))
		sb.WriteString("\n")
	}

	sb.WriteString(m.renderTotals())
	sb.WriteString("\n\n")
	sb.WriteString(m.renderQuickHelp())

	return sb.String()
}

func (m ReportsModel) renderHeader() string {
	var rangeStr string
	switch {
	case m.dateFrom == "" && m.dateTo == "":
		rangeStr = "All time"
	case m.dateFrom == m.dateTo && m.dateFrom != "":
		rangeStr = m.dateFrom
	default:
		from := m.dateFrom
		if from == "" {
			from = "—"
		}
		to := m.dateTo
		if to == "" {
			to = "—"
		}
		rangeStr = from + "  →  " + to
	}

	return StyleTitle.Render("Reports") +
		StyleMuted.Render("  ·  ") +
		StyleMuted.Render(rangeStr)
}

func (m ReportsModel) renderFilterBar() string {
	fromColor := cBorder
	toColor := cBorder
	if m.filterIdx == 0 {
		fromColor = cPrimary
	} else {
		toColor = cPrimary
	}

	fromInput := lipgloss.JoinHorizontal(lipgloss.Center,
		StyleFormLabel.Render("From"),
		lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(fromColor).
			Foreground(cText).
			Width(14).
			Render(m.fromInput.View()),
	)

	toInput := lipgloss.JoinHorizontal(lipgloss.Center,
		StyleFormLabel.Render("To  "),
		lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(toColor).
			Foreground(cText).
			Width(14).
			Render(m.toInput.View()),
	)

	return lipgloss.JoinHorizontal(lipgloss.Center,
		fromInput,
		StyleMuted.Render("    "),
		toInput,
		StyleHelp.Render("    tab switch  ·  enter apply  ·  esc cancel"),
	)
}

func (m ReportsModel) renderTable() string {
	totalW := rColGutter + rColClient + rColProject + rColHours + rColEntries + rColInv + rColPaid + rColEarned

	hdr := RowPrefix(false) +
		StyleTableHeader.Width(rColClient).Render("CLIENT") +
		StyleTableHeader.Width(rColProject).Render("PROJECT") +
		StyleTableHeader.Width(rColHours).Render("HOURS") +
		StyleTableHeader.Width(rColEntries).Render("ENTRIES") +
		StyleTableHeader.Width(rColInv).Render("INVOICED") +
		StyleTableHeader.Width(rColPaid).Render("PAID") +
		StyleTableHeader.Width(rColEarned).Render("EARNED")


	divider := StyleTableDiv.Render(strings.Repeat("─", totalW))

	var rows []string
	rows = append(rows, hdr, divider)
	for i, r := range m.rows {
		rows = append(rows, m.renderRow(r, i == m.cursor))
	}

	return strings.Join(rows, "\n")
}

func (m ReportsModel) renderRow(r *model.ReportRow, selected bool) string {
	prefix := RowPrefix(selected)

	hoursStr   := fmt.Sprintf("%.2fh", r.TotalHours)
	entriesStr := fmt.Sprintf("%d", r.EntryCount)
	invStr     := fmt.Sprintf("%d/%d", r.Invoiced, r.EntryCount)
	paidStr    := fmt.Sprintf("%d/%d", r.Paid, r.EntryCount)
	earnedStr  := fmt.Sprintf("$%.2f", r.Earnings)

	var base lipgloss.Style
	if selected {
		base = StyleTableRowSel
	} else {
		base = StyleTableRow
	}

	// colour-coded sub-columns only when not selected
	earnedStyle := base
	invStyle    := base
	paidStyle   := base
	hoursStyle  := base

	if !selected {
		hoursStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#A78BFA"))
		if r.Earnings > 0 {
			earnedStyle = StyleSuccess
		} else {
			earnedStyle = StyleMuted
		}
		switch {
		case r.Invoiced == r.EntryCount && r.EntryCount > 0:
			invStyle = StyleAccent
		case r.Invoiced > 0:
			invStyle = StyleWarning
		default:
			invStyle = StyleMuted
		}
		switch {
		case r.Paid == r.EntryCount && r.EntryCount > 0:
			paidStyle = StyleSuccess
		case r.Paid > 0:
			paidStyle = StyleWarning
		default:
			paidStyle = StyleMuted
		}
	}

	return prefix +
		base.Width(rColClient).Render(truncate(r.ClientName, rColClient-3)) +
		base.Width(rColProject).Render(truncate(r.ProjectName, rColProject-3)) +
		hoursStyle.Width(rColHours).Render(hoursStr) +
		base.Width(rColEntries).Render(entriesStr) +
		invStyle.Width(rColInv).Render(invStr) +
		paidStyle.Width(rColPaid).Render(paidStr) +
		earnedStyle.Width(rColEarned).Render(earnedStr)
}

func (m ReportsModel) renderTotals() string {
	var totalHours, totalEarnings float64
	var totalEntries, totalInvoiced, totalPaid int
	for _, r := range m.rows {
		totalHours    += r.TotalHours
		totalEarnings += r.Earnings
		totalEntries  += r.EntryCount
		totalInvoiced += r.Invoiced
		totalPaid     += r.Paid
	}

	totalW := rColGutter + rColClient + rColProject + rColHours + rColEntries + rColInv + rColPaid + rColEarned
	divider := StyleTableDiv.Render(strings.Repeat("─", totalW))

	totals := RowPrefix(false) +
		StyleTableHeader.Width(rColClient+rColProject).Render("TOTALS") +
		StyleTableHeader.Copy().Foreground(lipgloss.Color("#A78BFA")).Width(rColHours).Render(fmt.Sprintf("%.2fh", totalHours)) +
		StyleTableHeader.Width(rColEntries).Render(fmt.Sprintf("%d", totalEntries)) +
		StyleTableHeader.Width(rColInv).Render(fmt.Sprintf("%d/%d", totalInvoiced, totalEntries)) +
		StyleTableHeader.Width(rColPaid).Render(fmt.Sprintf("%d/%d", totalPaid, totalEntries)) +
		StyleTableHeader.Copy().Foreground(lipgloss.Color("#10B981")).Width(rColEarned).Render(fmt.Sprintf("$%.2f", totalEarnings))

	cards := m.renderSummaryCards(totalHours, totalEarnings, totalEntries, totalInvoiced, totalPaid)

	return lipgloss.JoinVertical(lipgloss.Left, divider, totals, "", cards)
}

func (m ReportsModel) renderSummaryCards(hours, earned float64, entries, invoiced, paid int) string {
	outstanding := earned
	for _, r := range m.rows {
		if r.Paid == r.EntryCount && r.EntryCount > 0 {
			outstanding -= r.Earnings
		}
	}
	unpaidEntries := entries - paid

	card := func(label, value string, vs lipgloss.Style) string {
		return lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(cBorder).
			PaddingLeft(2).
			MarginRight(4).
			Render(lipgloss.JoinVertical(lipgloss.Left,
				StyleMuted.Render(label),
				vs.Bold(true).Render(value),
			))
	}

	uninvStyle := StyleDanger
	if unpaidEntries == 0 {
		uninvStyle = StyleSuccess
	}

	return lipgloss.JoinHorizontal(lipgloss.Top,
		card("Total Hours",  fmt.Sprintf("%.2f h", hours),            lipgloss.NewStyle().Foreground(lipgloss.Color("#A78BFA"))),
		card("Total Earned", fmt.Sprintf("$%.2f", earned),            StyleSuccess),
		card("Outstanding",  fmt.Sprintf("$%.2f", outstanding),       StyleWarning),
		card("Uninvoiced",   fmt.Sprintf("%d entries", unpaidEntries), uninvStyle),
	)
}

// renderEntries shows the individual time entries for the selected report row.
func (m ReportsModel) renderEntries(r *model.ReportRow) string {
	if len(r.Entries) == 0 {
		return ""
	}

	var sb strings.Builder

	// Section header
	title := StyleSubtitle.Render(r.ClientName+" › "+r.ProjectName) +
		StyleMuted.Render("  —  tasks")
	sb.WriteString(title + "\n")

	// Column widths for the entry sub-table
	const (
		eColGutter = 2
		eColDate   = 14
		eColTask   = 36
		eColDur    = 12
		eColEarned = 12
		eColStatus = 12
	)

	totalW := eColGutter + eColDate + eColTask + eColDur + eColEarned + eColStatus
	sb.WriteString(StyleTableDiv.Render(strings.Repeat("─", totalW)) + "\n")

	hdr := RowPrefix(false) +
		StyleTableHeader.Width(eColDate).Render("DATE") +
		StyleTableHeader.Width(eColTask).Render("TASK") +
		StyleTableHeader.Width(eColDur).Render("DURATION") +
		StyleTableHeader.Width(eColEarned).Render("EARNED") +
		StyleTableHeader.Width(eColStatus).Render("STATUS")
	sb.WriteString(hdr + "\n")

	for _, e := range r.Entries {
		date := e.StartTime.Local().Format("Jan 2, 2006")
		task := truncate(e.Task, eColTask-3)
		dur := model.FormatDuration(e.Duration())

		var earnedStr string
		if r.HourlyRate > 0 {
			earnedStr = fmt.Sprintf("$%.2f", e.Earnings(r.HourlyRate))
		} else {
			earnedStr = "—"
		}

		var statusStr string
		switch {
		case e.Paid:
			statusStr = StyleSuccess.Render("paid")
		case e.Invoiced:
			statusStr = StyleAccent.Render("invoiced")
		default:
			statusStr = StyleMuted.Render("pending")
		}

		row := RowPrefix(false) +
			StyleTableRow.Width(eColDate).Render(date) +
			StyleTableRow.Width(eColTask).Render(task) +
			StyleTableRow.Width(eColDur).Render(dur) +
			StyleTableRow.Width(eColEarned).Render(earnedStr) +
			statusStr

		sb.WriteString(row + "\n")
	}

	return sb.String()
}

func (m ReportsModel) renderQuickHelp() string {
	return StyleHelp.Render("↑/↓ navigate  ·  f filter  ·  t today  ·  w week  ·  m month  ·  y year  ·  a all")
}
