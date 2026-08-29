package views

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aliasproject/notch/internal/db"
	"github.com/aliasproject/notch/internal/model"
	tea "github.com/charmbracelet/bubbletea"
	_ "modernc.org/sqlite"
)

// ── "create a client first" gate ─────────────────────────────────────────────

func TestProjectsUpdateList_NewRequiresAClient(t *testing.T) {
	m := ProjectsModel{}
	got, _ := m.updateList(runeKey('n'))
	if got.mode != projectsModeList {
		t.Errorf("mode after 'n' with no clients = %v, want to stay projectsModeList", got.mode)
	}
	if got.err == "" {
		t.Error("want an error telling the user to create a client first")
	}
}

func TestProjectsUpdateList_NewOpensFormWhenClientsExist(t *testing.T) {
	m := ProjectsModel{clients: []*model.Client{{ID: 1, Name: "Acme"}}}
	got, _ := m.updateList(runeKey('n'))
	if got.mode != projectsModeNewProject {
		t.Errorf("mode after 'n' with clients present = %v, want projectsModeNewProject", got.mode)
	}
	if got.err != "" {
		t.Errorf("err = %q, want cleared", got.err)
	}
}

// ── resolveClientID ──────────────────────────────────────────────────────────

func TestResolveClientID(t *testing.T) {
	clients := []*model.Client{{ID: 1, Name: "Acme"}, {ID: 2, Name: "Beta"}}

	t.Run("already resolved is a no-op", func(t *testing.T) {
		m := ProjectsModel{clients: clients, clientID: 2}
		if err := m.resolveClientID(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if m.clientID != 2 {
			t.Errorf("clientID changed to %d, want unchanged 2", m.clientID)
		}
	})

	t.Run("blank input requires a selection", func(t *testing.T) {
		inputs := makeProjectInputs(nil, "")
		m := ProjectsModel{clients: clients, inputs: inputs}
		if err := m.resolveClientID(); err == nil {
			t.Error("want an error for a blank client field")
		}
	})

	t.Run("exact case-insensitive match resolves", func(t *testing.T) {
		inputs := makeProjectInputs(nil, "")
		inputs[projFieldClient].SetValue("acme")
		m := ProjectsModel{clients: clients, inputs: inputs}
		if err := m.resolveClientID(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m.clientID != 1 {
			t.Errorf("clientID = %d, want 1 (Acme)", m.clientID)
		}
	})

	t.Run("no match is an error", func(t *testing.T) {
		inputs := makeProjectInputs(nil, "")
		inputs[projFieldClient].SetValue("Gamma")
		m := ProjectsModel{clients: clients, inputs: inputs}
		if err := m.resolveClientID(); err == nil {
			t.Error("want an error when the typed text matches no existing client")
		}
	})
}

// ── saveForm validation (pure — returns before touching the DB) ──────────────

func TestProjectsSaveForm_BlankNameRejected(t *testing.T) {
	inputs := makeProjectInputs(nil, "Acme")
	inputs[projFieldName].SetValue("   ")
	m := ProjectsModel{mode: projectsModeNewProject, clients: []*model.Client{{ID: 1, Name: "Acme"}}, clientID: 1, inputs: inputs}

	got, cmd := m.saveForm()
	if got.err == "" {
		t.Error("want a validation error for a blank name")
	}
	if cmd != nil {
		t.Error("want nil cmd on validation failure (no DB touch)")
	}
}

func TestProjectsSaveForm_UnresolvedClientRejected(t *testing.T) {
	inputs := makeProjectInputs(nil, "")
	inputs[projFieldName].SetValue("Website")
	inputs[projFieldClient].SetValue("Nonexistent")
	m := ProjectsModel{mode: projectsModeNewProject, clients: []*model.Client{{ID: 1, Name: "Acme"}}, inputs: inputs}

	got, cmd := m.saveForm()
	if got.err == "" {
		t.Error("want a validation error when the client doesn't resolve")
	}
	if cmd != nil {
		t.Error("want nil cmd on validation failure (no DB touch)")
	}
}

// ── Confirm-delete dispatch ───────────────────────────────────────────────────

func TestProjectsUpdateForm_ConfirmDeleteDispatch(t *testing.T) {
	d := newTestDB(t)
	c, err := d.CreateClient("Acme", 100)
	if err != nil {
		t.Fatal(err)
	}
	p, err := d.CreateProject(c.ID, "Website")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("y deletes", func(t *testing.T) {
		m := ProjectsModel{db: d, mode: projectsModeConfirmDelete, projects: []*model.Project{p}, cursor: 0}
		got, cmd := m.updateForm(runeKey('y'))
		if got.mode != projectsModeList {
			t.Errorf("mode after 'y' = %v, want projectsModeList", got.mode)
		}
		if cmd == nil {
			t.Fatal("want a non-nil cmd after deleting")
		}
		cmd()
		projects, _ := d.ListProjects(0)
		if len(projects) != 0 {
			t.Errorf("projects after delete = %+v, want none", projects)
		}
	})

	t.Run("n cancels", func(t *testing.T) {
		p2, err := d.CreateProject(c.ID, "App")
		if err != nil {
			t.Fatal(err)
		}
		m := ProjectsModel{db: d, mode: projectsModeConfirmDelete, projects: []*model.Project{p2}, cursor: 0}
		got, cmd := m.updateForm(runeKey('n'))
		if got.mode != projectsModeList {
			t.Errorf("mode after 'n' = %v, want projectsModeList", got.mode)
		}
		if cmd != nil {
			t.Error("want nil cmd when cancelling delete")
		}
		projects, _ := d.ListProjects(0)
		if len(projects) != 1 {
			t.Errorf("project should not be deleted on cancel, got %d projects", len(projects))
		}
	})

	t.Run("esc cancels", func(t *testing.T) {
		m := ProjectsModel{db: d, mode: projectsModeConfirmDelete}
		got, cmd := m.updateForm(tea.KeyMsg{Type: tea.KeyEsc})
		if got.mode != projectsModeList {
			t.Errorf("mode after esc = %v, want projectsModeList", got.mode)
		}
		if cmd != nil {
			t.Error("want nil cmd on esc")
		}
	})
}

// ── Dropdown ──────────────────────────────────────────────────────────────────

func TestProjectsRebuildClientMatches(t *testing.T) {
	clients := []*model.Client{{ID: 1, Name: "Acme Corp"}, {ID: 2, Name: "Beta LLC"}}
	inputs := makeProjectInputs(nil, "")
	m := ProjectsModel{clients: clients, inputs: inputs}

	m.inputs[projFieldClient].SetValue("acme")
	m.rebuildClientMatches()
	if len(m.clientMatches) != 1 || m.clientMatches[0].id != 1 {
		t.Errorf("clientMatches = %+v, want just Acme Corp (no synthetic create option in projects.go)", m.clientMatches)
	}

	m.inputs[projFieldClient].SetValue("")
	m.rebuildClientMatches()
	if len(m.clientMatches) != 2 {
		t.Errorf("clientMatches with empty query = %+v, want both clients", m.clientMatches)
	}
}

func TestProjectsApplyClientSelection(t *testing.T) {
	clients := []*model.Client{{ID: 1, Name: "Acme Corp"}}
	inputs := makeProjectInputs(nil, "")
	m := ProjectsModel{clients: clients, inputs: inputs}
	m.inputs[projFieldClient].SetValue("Acme")
	m.rebuildClientMatches()
	m.clientSel = 0
	m.applyClientSelection()

	if m.clientID != 1 {
		t.Errorf("clientID = %d, want 1", m.clientID)
	}
	if m.inputs[projFieldClient].Value() != "Acme Corp" {
		t.Errorf("client input = %q, want %q", m.inputs[projFieldClient].Value(), "Acme Corp")
	}
	if m.showClientDrop {
		t.Error("dropdown should close after applying a selection")
	}
}

// ── selectedProject / listColWidths ──────────────────────────────────────────

func TestProjectsSelectedProject(t *testing.T) {
	m := ProjectsModel{}
	if got := m.selectedProject(); got != nil {
		t.Errorf("selectedProject() on empty list = %v, want nil", got)
	}

	p := &model.Project{ID: 1, Name: "Website"}
	m2 := ProjectsModel{projects: []*model.Project{p}, cursor: 0}
	if got := m2.selectedProject(); got != p {
		t.Errorf("selectedProject() = %v, want %v", got, p)
	}
}

func TestProjectsListColWidths_SpansAvailableWidth(t *testing.T) {
	m := ProjectsModel{width: 100}
	nameCol, clientCol := m.listColWidths()
	if nameCol < 18 {
		t.Errorf("nameCol = %d, want >= 18", nameCol)
	}
	if clientCol < 18 || clientCol > 36 {
		t.Errorf("clientCol = %d, want within [18, 36]", clientCol)
	}
}

func TestProjectsListColWidths_NarrowClampsBothFloors(t *testing.T) {
	// avail = usableWidth(30) - 2 = 24 - 2 = 22; clientCol = 22/3 = 7 -> clamps
	// to 18, then nameCol = 22-18 = 4 -> also clamps to 18.
	m := ProjectsModel{width: 30}
	nameCol, clientCol := m.listColWidths()
	if clientCol != 18 {
		t.Errorf("clientCol = %d, want 18 (low clamp)", clientCol)
	}
	if nameCol != 18 {
		t.Errorf("nameCol = %d, want 18 (low clamp)", nameCol)
	}
}

func TestProjectsListColWidths_WideClampsClientHigh(t *testing.T) {
	// avail = usableWidth(200) - 2 = 194 - 2 = 192; clientCol = 192/3 = 64 ->
	// clamps to 36.
	m := ProjectsModel{width: 200}
	nameCol, clientCol := m.listColWidths()
	if clientCol != 36 {
		t.Errorf("clientCol = %d, want 36 (high clamp)", clientCol)
	}
	if nameCol < 18 {
		t.Errorf("nameCol = %d, want >= 18", nameCol)
	}
}

// ── Trivial constructors / getters ───────────────────────────────────────────

func TestNewProjects(t *testing.T) {
	d := newTestDB(t)
	m := NewProjects(d)
	if m.mode != projectsModeList {
		t.Errorf("mode = %v, want projectsModeList", m.mode)
	}
	if m.db != d {
		t.Error("db not stored on the model")
	}
}

func TestProjectsIsBusy(t *testing.T) {
	list := ProjectsModel{mode: projectsModeList}
	if list.IsBusy() {
		t.Error("IsBusy() in list mode = true, want false")
	}
	form := ProjectsModel{mode: projectsModeNewProject}
	if !form.IsBusy() {
		t.Error("IsBusy() in new-project mode = false, want true")
	}
}

func TestProjectsHelp(t *testing.T) {
	cases := []struct {
		name string
		mode projectsMode
	}{
		{"new", projectsModeNewProject},
		{"edit", projectsModeEditProject},
		{"confirm delete", projectsModeConfirmDelete},
		{"list", projectsModeList},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := ProjectsModel{mode: c.mode}
			if got := m.Help(); len(got) == 0 {
				t.Error("Help() returned no hotkeys")
			}
		})
	}
}

func TestProjectsInit(t *testing.T) {
	d := newTestDB(t)
	m := NewProjects(d)
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init() returned a nil cmd")
	}
	msg := cmd()
	if _, ok := msg.(projectsDataMsg); !ok {
		t.Errorf("Init() cmd produced %T, want projectsDataMsg", msg)
	}
}

func TestProjectsSetSize(t *testing.T) {
	m := &ProjectsModel{}
	m.SetSize(80, 24)
	if m.width != 80 || m.height != 24 {
		t.Errorf("SetSize(80, 24) -> width=%d height=%d, want 80, 24", m.width, m.height)
	}
}

// ── loadProjectsData ──────────────────────────────────────────────────────────

func TestLoadProjectsData_Success(t *testing.T) {
	d := newTestDB(t)
	c, err := d.CreateClient("Acme", 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateProject(c.ID, "Website"); err != nil {
		t.Fatal(err)
	}

	cmd := loadProjectsData(d)
	msg := cmd()
	data, ok := msg.(projectsDataMsg)
	if !ok {
		t.Fatalf("loadProjectsData produced %T, want projectsDataMsg", msg)
	}
	if len(data.clients) != 1 {
		t.Errorf("clients = %+v, want 1", data.clients)
	}
	if len(data.projects) != 1 {
		t.Errorf("projects = %+v, want 1", data.projects)
	}
}

func TestLoadProjectsData_ListClientsError(t *testing.T) {
	d := newTestDB(t)
	d.Close()

	cmd := loadProjectsData(d)
	msg := cmd()
	if _, ok := msg.(ErrMsg); !ok {
		t.Errorf("loadProjectsData on a closed db produced %T, want ErrMsg", msg)
	}
}

func TestLoadProjectsData_ListProjectsError(t *testing.T) {
	// Isolate the second `if err != nil` branch in loadProjectsData (the
	// ListProjects error path) from the first (the ListClients error path):
	// open a second raw connection to the same sqlite file and drop the
	// projects table out from under it, so ListClients still succeeds
	// (clients table is untouched) while ListProjects fails.
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := raw.Exec("DROP TABLE projects"); err != nil {
		t.Fatalf("drop table projects: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw connection: %v", err)
	}

	cmd := loadProjectsData(d)
	msg := cmd()
	if _, ok := msg.(ErrMsg); !ok {
		t.Fatalf("loadProjectsData with projects table missing produced %T, want ErrMsg", msg)
	}
}

// ── Update dispatch ───────────────────────────────────────────────────────────

func TestProjectsUpdate_DataMsgClampsCursor(t *testing.T) {
	m := ProjectsModel{cursor: 5, err: "stale error"}
	got, cmd := m.Update(projectsDataMsg{
		clients:  []*model.Client{{ID: 1, Name: "Acme"}},
		projects: []*model.Project{{ID: 1, Name: "A"}, {ID: 2, Name: "B"}},
	})
	if cmd != nil {
		t.Error("want nil cmd from projectsDataMsg handling")
	}
	if got.cursor != 1 {
		t.Errorf("cursor = %d, want clamped to len(projects)-1 = 1", got.cursor)
	}
	if got.err != "" {
		t.Errorf("err = %q, want cleared", got.err)
	}
}

func TestProjectsUpdate_DataMsgCursorZeroStaysZero(t *testing.T) {
	m := ProjectsModel{cursor: 0}
	got, _ := m.Update(projectsDataMsg{})
	if got.cursor != 0 {
		t.Errorf("cursor = %d, want 0 (cursor > 0 guard skips clamp)", got.cursor)
	}
}

func TestProjectsUpdate_KeyDispatchesToList(t *testing.T) {
	m := ProjectsModel{mode: projectsModeList, projects: []*model.Project{{ID: 1}, {ID: 2}}}
	got, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got.cursor != 1 {
		t.Errorf("cursor = %d, want 1 (Update dispatched to updateList)", got.cursor)
	}
}

func TestProjectsUpdate_KeyDispatchesToForm(t *testing.T) {
	m := ProjectsModel{mode: projectsModeNewProject, inputs: makeProjectInputs(nil, "")}
	got, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if got.mode != projectsModeList {
		t.Errorf("mode = %v, want projectsModeList (Update dispatched to updateForm)", got.mode)
	}
}

func TestProjectsUpdate_UnhandledMsgTypeIsNoop(t *testing.T) {
	m := ProjectsModel{cursor: 3}
	got, cmd := m.Update(StatusMsg("irrelevant"))
	if cmd != nil {
		t.Error("want nil cmd for an unhandled message type")
	}
	if got.cursor != 3 {
		t.Errorf("cursor = %d, want unchanged", got.cursor)
	}
}

// ── updateList ────────────────────────────────────────────────────────────────

func TestProjectsUpdateList_CursorBounds(t *testing.T) {
	m := ProjectsModel{projects: []*model.Project{{ID: 1}, {ID: 2}}}

	m, _ = m.updateList(tea.KeyMsg{Type: tea.KeyUp})
	if m.cursor != 0 {
		t.Errorf("cursor after up at top = %d, want 0 (clamped)", m.cursor)
	}

	m, _ = m.updateList(tea.KeyMsg{Type: tea.KeyDown})
	m, _ = m.updateList(tea.KeyMsg{Type: tea.KeyDown})
	if m.cursor != 1 {
		t.Errorf("cursor after two downs with 2 projects = %d, want 1 (clamped at end)", m.cursor)
	}

	m, _ = m.updateList(tea.KeyMsg{Type: tea.KeyUp})
	if m.cursor != 0 {
		t.Errorf("cursor after up with cursor > 0 = %d, want decremented to 0", m.cursor)
	}
}

func TestProjectsUpdateList_EditWithClientSet(t *testing.T) {
	p := &model.Project{ID: 1, Name: "Website", Client: &model.Client{ID: 9, Name: "Acme"}}
	m := ProjectsModel{projects: []*model.Project{p}, cursor: 0}
	got, _ := m.updateList(runeKey('e'))
	if got.mode != projectsModeEditProject {
		t.Fatalf("mode = %v, want projectsModeEditProject", got.mode)
	}
	if got.clientID != 9 {
		t.Errorf("clientID = %d, want 9", got.clientID)
	}
	if got.inputs[projFieldClient].Value() != "Acme" {
		t.Errorf("client input = %q, want %q", got.inputs[projFieldClient].Value(), "Acme")
	}
	if got.inputs[projFieldName].Value() != "Website" {
		t.Errorf("name input = %q, want %q", got.inputs[projFieldName].Value(), "Website")
	}
}

func TestProjectsUpdateList_EditWithNilClientFallsBack(t *testing.T) {
	p := &model.Project{ID: 1, Name: "Website", Client: nil}
	m := ProjectsModel{projects: []*model.Project{p}, cursor: 0}
	got, _ := m.updateList(runeKey('e'))
	if got.mode != projectsModeEditProject {
		t.Fatalf("mode = %v, want projectsModeEditProject", got.mode)
	}
	if got.clientID != 0 {
		t.Errorf("clientID = %d, want 0 (no client to resolve)", got.clientID)
	}
	if got.inputs[projFieldClient].Value() != "" {
		t.Errorf("client input = %q, want empty", got.inputs[projFieldClient].Value())
	}
}

func TestProjectsUpdateList_EditWithNoProjectsIsNoop(t *testing.T) {
	m := ProjectsModel{}
	got, _ := m.updateList(runeKey('e'))
	if got.mode != projectsModeList {
		t.Errorf("mode = %v, want to stay projectsModeList when there's nothing to edit", got.mode)
	}
}

func TestProjectsUpdateList_DeleteWithSelection(t *testing.T) {
	p := &model.Project{ID: 1, Name: "Website"}
	m := ProjectsModel{projects: []*model.Project{p}, cursor: 0}
	got, _ := m.updateList(runeKey('d'))
	if got.mode != projectsModeConfirmDelete {
		t.Errorf("mode = %v, want projectsModeConfirmDelete", got.mode)
	}
	if got.deleteTarget == "" {
		t.Error("want a non-empty deleteTarget")
	}
}

func TestProjectsUpdateList_DeleteWithoutSelectionIsNoop(t *testing.T) {
	m := ProjectsModel{}
	got, _ := m.updateList(runeKey('d'))
	if got.mode != projectsModeList {
		t.Errorf("mode = %v, want to stay projectsModeList with no projects to delete", got.mode)
	}
}

// ── updateForm ────────────────────────────────────────────────────────────────

func TestProjectsUpdateForm_ClientDropdownNavigation(t *testing.T) {
	clients := []*model.Client{{ID: 1, Name: "Acme"}, {ID: 2, Name: "Beta"}}
	inputs := makeProjectInputs(nil, "")
	m := ProjectsModel{
		mode:         projectsModeNewProject,
		clients:      clients,
		inputs:       inputs,
		focusedInput: projFieldClient,
	}
	m.rebuildClientMatches()

	// Down moves selection forward and shows the dropdown.
	got, _ := m.updateForm(tea.KeyMsg{Type: tea.KeyDown})
	if got.clientSel != 1 {
		t.Errorf("clientSel after down = %d, want 1", got.clientSel)
	}
	if !got.showClientDrop {
		t.Error("showClientDrop should be true after dropdown navigation")
	}

	// Down again is clamped at the last match.
	got, _ = got.updateForm(tea.KeyMsg{Type: tea.KeyDown})
	if got.clientSel != 1 {
		t.Errorf("clientSel after second down = %d, want clamped at 1", got.clientSel)
	}

	// Up moves selection back.
	got, _ = got.updateForm(tea.KeyMsg{Type: tea.KeyUp})
	if got.clientSel != 0 {
		t.Errorf("clientSel after up = %d, want 0", got.clientSel)
	}

	// Up again is clamped at 0.
	got, _ = got.updateForm(tea.KeyMsg{Type: tea.KeyUp})
	if got.clientSel != 0 {
		t.Errorf("clientSel after up at top = %d, want clamped at 0", got.clientSel)
	}
}

func TestProjectsUpdateForm_ConfirmOnClientAppliesSelection(t *testing.T) {
	clients := []*model.Client{{ID: 1, Name: "Acme"}}
	inputs := makeProjectInputs(nil, "")
	m := ProjectsModel{
		mode:         projectsModeNewProject,
		clients:      clients,
		inputs:       inputs,
		focusedInput: projFieldClient,
	}
	m.inputs[projFieldClient].Focus()
	m.rebuildClientMatches()

	got, cmd := m.updateForm(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Error("want nil cmd when confirming a client selection")
	}
	if got.clientID != 1 {
		t.Errorf("clientID = %d, want 1", got.clientID)
	}
	if got.focusedInput != projFieldName {
		t.Errorf("focusedInput = %d, want projFieldName", got.focusedInput)
	}
	if got.showClientDrop {
		t.Error("showClientDrop should be hidden after confirming a client selection")
	}
	if !got.inputs[projFieldName].Focused() {
		t.Error("name input should be focused after confirming a client selection")
	}
	if got.inputs[projFieldClient].Focused() {
		t.Error("client input should be blurred after confirming a client selection")
	}
}

func TestProjectsUpdateForm_ConfirmOnNameDispatchesSave(t *testing.T) {
	inputs := makeProjectInputs(nil, "")
	// Leave the name blank so saveForm hits its cheap validation-error return
	// rather than touching the DB.
	m := ProjectsModel{
		mode:         projectsModeNewProject,
		clients:      []*model.Client{{ID: 1, Name: "Acme"}},
		clientID:     1,
		inputs:       inputs,
		focusedInput: projFieldName,
	}
	got, _ := m.updateForm(tea.KeyMsg{Type: tea.KeyEnter})
	if got.err == "" {
		t.Error("want saveForm's blank-name validation error to surface via updateForm dispatch")
	}
}

func TestProjectsUpdateForm_Cancel(t *testing.T) {
	m := ProjectsModel{mode: projectsModeNewProject, inputs: makeProjectInputs(nil, ""), err: "boom"}
	got, cmd := m.updateForm(tea.KeyMsg{Type: tea.KeyEsc})
	if got.mode != projectsModeList {
		t.Errorf("mode = %v, want projectsModeList", got.mode)
	}
	if got.err != "" {
		t.Errorf("err = %q, want cleared", got.err)
	}
	if cmd != nil {
		t.Error("want nil cmd on cancel")
	}
}

func TestProjectsUpdateForm_NextFieldTogglesFocus(t *testing.T) {
	clients := []*model.Client{{ID: 1, Name: "Acme"}}
	inputs := makeProjectInputs(nil, "")
	m := ProjectsModel{mode: projectsModeNewProject, clients: clients, inputs: inputs, focusedInput: projFieldClient}
	m.inputs[projFieldClient].Focus()

	got, _ := m.updateForm(tea.KeyMsg{Type: tea.KeyTab})
	if got.focusedInput != projFieldName {
		t.Errorf("focusedInput after tab from client = %d, want projFieldName", got.focusedInput)
	}
	if !got.inputs[projFieldName].Focused() {
		t.Error("name input should be focused after tab")
	}
	if got.showClientDrop {
		t.Error("dropdown should be hidden once focus leaves the client field")
	}

	got2, _ := got.updateForm(tea.KeyMsg{Type: tea.KeyTab})
	if got2.focusedInput != projFieldClient {
		t.Errorf("focusedInput after tab from name = %d, want projFieldClient", got2.focusedInput)
	}
	if !got2.showClientDrop {
		t.Error("dropdown should reappear once focus returns to the client field with matches available")
	}
}

func TestProjectsUpdateForm_PrevFieldTogglesFocus(t *testing.T) {
	clients := []*model.Client{{ID: 1, Name: "Acme"}}
	inputs := makeProjectInputs(nil, "")
	m := ProjectsModel{mode: projectsModeNewProject, clients: clients, inputs: inputs, focusedInput: projFieldName}
	m.inputs[projFieldName].Focus()

	got, _ := m.updateForm(tea.KeyMsg{Type: tea.KeyShiftTab})
	if got.focusedInput != projFieldClient {
		t.Errorf("focusedInput after shift+tab from name = %d, want projFieldClient", got.focusedInput)
	}

	got2, _ := got.updateForm(tea.KeyMsg{Type: tea.KeyShiftTab})
	if got2.focusedInput != projFieldName {
		t.Errorf("focusedInput after shift+tab from client = %d, want projFieldName", got2.focusedInput)
	}
}

func TestProjectsUpdateForm_DefaultForwardsToFocusedInput(t *testing.T) {
	clients := []*model.Client{{ID: 1, Name: "Acme"}}
	inputs := makeProjectInputs(nil, "")
	m := ProjectsModel{
		mode:         projectsModeNewProject,
		clients:      clients,
		inputs:       inputs,
		focusedInput: projFieldClient,
		clientID:     1,
	}
	m.inputs[projFieldClient].Focus()

	got, _ := m.updateForm(runeKey('a'))
	if got.inputs[projFieldClient].Value() != "a" {
		t.Errorf("client input = %q, want %q", got.inputs[projFieldClient].Value(), "a")
	}
	if got.clientID != 0 {
		t.Errorf("clientID = %d, want reset to 0 after editing the client field", got.clientID)
	}

	// Typing into the name field should not touch clientID or the dropdown.
	m2 := ProjectsModel{
		mode:         projectsModeNewProject,
		clients:      clients,
		inputs:       makeProjectInputs(nil, ""),
		focusedInput: projFieldName,
		clientID:     1,
	}
	m2.inputs[projFieldName].Focus()
	got2, _ := m2.updateForm(runeKey('x'))
	if got2.inputs[projFieldName].Value() != "x" {
		t.Errorf("name input = %q, want %q", got2.inputs[projFieldName].Value(), "x")
	}
	if got2.clientID != 1 {
		t.Errorf("clientID = %d, want unchanged when typing in the name field", got2.clientID)
	}
}

// ── saveForm (DB round trips) ─────────────────────────────────────────────────

func TestProjectsSaveForm_CreateSuccess(t *testing.T) {
	d := newTestDB(t)
	c, err := d.CreateClient("Acme", 100)
	if err != nil {
		t.Fatal(err)
	}
	inputs := makeProjectInputs(nil, "")
	inputs[projFieldName].SetValue("Website")
	m := ProjectsModel{db: d, mode: projectsModeNewProject, clients: []*model.Client{c}, clientID: c.ID, inputs: inputs}

	got, cmd := m.saveForm()
	if got.mode != projectsModeList {
		t.Errorf("mode = %v, want projectsModeList after successful save", got.mode)
	}
	if cmd == nil {
		t.Fatal("want a non-nil cmd on successful create")
	}
	projects, err := d.ListProjects(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].Name != "Website" {
		t.Errorf("projects = %+v, want one project named Website", projects)
	}
}

func TestProjectsSaveForm_CreateError(t *testing.T) {
	d := newTestDB(t)
	c, err := d.CreateClient("Acme", 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateProject(c.ID, "Website"); err != nil {
		t.Fatal(err)
	}

	inputs := makeProjectInputs(nil, "")
	inputs[projFieldName].SetValue("Website") // duplicate (client, name)
	m := ProjectsModel{db: d, mode: projectsModeNewProject, clients: []*model.Client{c}, clientID: c.ID, inputs: inputs}

	got, cmd := m.saveForm()
	if got.err == "" {
		t.Error("want a DB error surfaced on duplicate project name")
	}
	if cmd != nil {
		t.Error("want nil cmd when CreateProject fails")
	}
	if got.mode != projectsModeNewProject {
		t.Errorf("mode = %v, want to stay projectsModeNewProject on error", got.mode)
	}
}

func TestProjectsSaveForm_EditSuccess(t *testing.T) {
	d := newTestDB(t)
	c, err := d.CreateClient("Acme", 100)
	if err != nil {
		t.Fatal(err)
	}
	p, err := d.CreateProject(c.ID, "Website")
	if err != nil {
		t.Fatal(err)
	}

	inputs := makeProjectInputs(p, c.Name)
	inputs[projFieldName].SetValue("Website Redesign")
	m := ProjectsModel{
		db: d, mode: projectsModeEditProject,
		clients: []*model.Client{c}, projects: []*model.Project{p}, cursor: 0,
		clientID: c.ID, inputs: inputs,
	}

	got, cmd := m.saveForm()
	if got.mode != projectsModeList {
		t.Errorf("mode = %v, want projectsModeList after successful update", got.mode)
	}
	if cmd == nil {
		t.Fatal("want a non-nil cmd on successful update")
	}
	updated, err := d.GetProject(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Website Redesign" {
		t.Errorf("project name = %q, want %q", updated.Name, "Website Redesign")
	}
}

func TestProjectsSaveForm_EditError(t *testing.T) {
	d := newTestDB(t)
	c, err := d.CreateClient("Acme", 100)
	if err != nil {
		t.Fatal(err)
	}
	p1, err := d.CreateProject(c.ID, "Website")
	if err != nil {
		t.Fatal(err)
	}
	p2, err := d.CreateProject(c.ID, "App")
	if err != nil {
		t.Fatal(err)
	}

	// Rename p2 to collide with p1's (client, name) pair.
	inputs := makeProjectInputs(p2, c.Name)
	inputs[projFieldName].SetValue("Website")
	m := ProjectsModel{
		db: d, mode: projectsModeEditProject,
		clients: []*model.Client{c}, projects: []*model.Project{p2}, cursor: 0,
		clientID: c.ID, inputs: inputs,
	}

	got, cmd := m.saveForm()
	if got.err == "" {
		t.Error("want a DB error surfaced on duplicate project name during update")
	}
	if cmd != nil {
		t.Error("want nil cmd when UpdateProject fails")
	}
	_ = p1
}

func TestProjectsSaveForm_EditWithNoSelectedProjectResetsMode(t *testing.T) {
	d := newTestDB(t)
	c, err := d.CreateClient("Acme", 100)
	if err != nil {
		t.Fatal(err)
	}
	inputs := makeProjectInputs(nil, "")
	inputs[projFieldName].SetValue("Website")
	m := ProjectsModel{
		db: d, mode: projectsModeEditProject,
		clients: []*model.Client{c}, projects: nil, cursor: 0,
		clientID: c.ID, inputs: inputs,
	}

	got, cmd := m.saveForm()
	if got.mode != projectsModeList {
		t.Errorf("mode = %v, want projectsModeList", got.mode)
	}
	if cmd != nil {
		t.Error("want nil cmd when there is no selected project to update")
	}
}

func TestProjectsSaveForm_UnknownModeFallsThrough(t *testing.T) {
	// saveForm's switch only handles projectsModeNewProject and
	// projectsModeEditProject; any other mode (unreachable via the normal UI
	// flow, since updateForm only calls saveForm from those two modes) falls
	// through to the trailing "return m, nil".
	inputs := makeProjectInputs(nil, "")
	inputs[projFieldName].SetValue("Website")
	m := ProjectsModel{mode: projectsModeList, clients: []*model.Client{{ID: 1, Name: "Acme"}}, clientID: 1, inputs: inputs}

	got, cmd := m.saveForm()
	if got.mode != projectsModeList {
		t.Errorf("mode = %v, want unchanged projectsModeList", got.mode)
	}
	if cmd != nil {
		t.Error("want nil cmd for a mode saveForm doesn't handle")
	}
}

// ── doDelete ──────────────────────────────────────────────────────────────────

func TestProjectsDoDelete_Success(t *testing.T) {
	d := newTestDB(t)
	c, err := d.CreateClient("Acme", 100)
	if err != nil {
		t.Fatal(err)
	}
	p1, err := d.CreateProject(c.ID, "Website")
	if err != nil {
		t.Fatal(err)
	}
	p2, err := d.CreateProject(c.ID, "App")
	if err != nil {
		t.Fatal(err)
	}

	m := ProjectsModel{db: d, mode: projectsModeConfirmDelete, projects: []*model.Project{p1, p2}, cursor: 1}
	got, cmd := m.doDelete()
	if got.mode != projectsModeList {
		t.Errorf("mode = %v, want projectsModeList", got.mode)
	}
	if got.cursor != 0 {
		t.Errorf("cursor = %d, want decremented to 0", got.cursor)
	}
	if cmd == nil {
		t.Fatal("want a non-nil cmd on successful delete")
	}
}

func TestProjectsDoDelete_CursorStaysAtZero(t *testing.T) {
	d := newTestDB(t)
	c, err := d.CreateClient("Acme", 100)
	if err != nil {
		t.Fatal(err)
	}
	p, err := d.CreateProject(c.ID, "Website")
	if err != nil {
		t.Fatal(err)
	}

	m := ProjectsModel{db: d, projects: []*model.Project{p}, cursor: 0}
	got, _ := m.doDelete()
	if got.cursor != 0 {
		t.Errorf("cursor = %d, want to stay 0", got.cursor)
	}
}

func TestProjectsDoDelete_NoSelectionIsNoop(t *testing.T) {
	m := ProjectsModel{mode: projectsModeConfirmDelete}
	got, cmd := m.doDelete()
	if got.mode != projectsModeList {
		t.Errorf("mode = %v, want projectsModeList", got.mode)
	}
	if cmd != nil {
		t.Error("want nil cmd when there is nothing to delete")
	}
}

func TestProjectsDoDelete_Error(t *testing.T) {
	d := newTestDB(t)
	c, err := d.CreateClient("Acme", 100)
	if err != nil {
		t.Fatal(err)
	}
	p, err := d.CreateProject(c.ID, "Website")
	if err != nil {
		t.Fatal(err)
	}
	d.Close()

	m := ProjectsModel{db: d, projects: []*model.Project{p}, cursor: 0}
	_, cmd := m.doDelete()
	if cmd == nil {
		t.Fatal("want a non-nil error cmd when DeleteProject fails")
	}
	msg := cmd()
	if _, ok := msg.(ErrMsg); !ok {
		t.Errorf("cmd() produced %T, want ErrMsg", msg)
	}
}

// ── rebuildClientMatches / applyClientSelection edge cases ───────────────────

func TestProjectsRebuildClientMatches_ClampsSelectionWhenMatchesShrink(t *testing.T) {
	clients := []*model.Client{{ID: 1, Name: "Acme Corp"}, {ID: 2, Name: "Acme Two"}}
	inputs := makeProjectInputs(nil, "")
	m := ProjectsModel{clients: clients, inputs: inputs}
	m.rebuildClientMatches() // 2 matches
	m.clientSel = 1

	m.inputs[projFieldClient].SetValue("nonexistent")
	m.rebuildClientMatches() // 0 matches now; clientSel must clamp back to 0
	if m.clientSel != 0 {
		t.Errorf("clientSel = %d, want clamped to 0 when matches shrink below the old selection", m.clientSel)
	}
}

func TestProjectsApplyClientSelection_NoMatchesIsNoop(t *testing.T) {
	inputs := makeProjectInputs(nil, "")
	m := ProjectsModel{inputs: inputs, showClientDrop: true}
	m.applyClientSelection()
	if m.clientID != 0 {
		t.Errorf("clientID = %d, want unchanged 0 when there are no matches", m.clientID)
	}
	if !m.showClientDrop {
		t.Error("showClientDrop should be left untouched by the early return")
	}
}

// ── View ──────────────────────────────────────────────────────────────────────

func TestProjectsView_DelegatesToFormWhenNotInListMode(t *testing.T) {
	m := ProjectsModel{mode: projectsModeNewProject, inputs: makeProjectInputs(nil, ""), width: 100}
	if got := m.View(); got == "" {
		t.Error("View() in form mode returned empty output")
	}
}

func TestProjectsView_NoClientsMessage(t *testing.T) {
	m := ProjectsModel{width: 100}
	got := m.View()
	if !strings.Contains(got, "No clients yet") {
		t.Errorf("View() = %q, want a message about creating a client first", got)
	}
}

func TestProjectsView_NoProjectsMessage(t *testing.T) {
	m := ProjectsModel{width: 100, clients: []*model.Client{{ID: 1, Name: "Acme"}}}
	got := m.View()
	if !strings.Contains(got, "No projects yet") {
		t.Errorf("View() = %q, want a message about adding a project", got)
	}
}

func TestProjectsView_ListWithClientAndWithoutClient(t *testing.T) {
	m := ProjectsModel{
		width:   100,
		clients: []*model.Client{{ID: 1, Name: "Acme"}},
		projects: []*model.Project{
			{ID: 1, Name: "Website", Client: &model.Client{ID: 1, Name: "Acme"}},
			{ID: 2, Name: "Orphan Project", Client: nil},
		},
		cursor: 0,
	}
	got := m.View()
	if !strings.Contains(got, "Website") || !strings.Contains(got, "Acme") {
		t.Errorf("View() missing project/client row: %q", got)
	}
	if !strings.Contains(got, "Orphan") {
		t.Errorf("View() missing nil-client project row: %q", got)
	}
}

func TestProjectsView_ErrorLine(t *testing.T) {
	m := ProjectsModel{width: 100, err: "something broke"}
	got := m.View()
	if !strings.Contains(got, "something broke") {
		t.Errorf("View() = %q, want the error message rendered", got)
	}
}

// ── viewForm ──────────────────────────────────────────────────────────────────

func TestProjectsViewForm_ConfirmDeleteDelegates(t *testing.T) {
	m := ProjectsModel{mode: projectsModeConfirmDelete, deleteTarget: `project "Website"`, width: 100}
	got := m.viewForm()
	if !strings.Contains(got, "Website") {
		t.Errorf("viewForm() in confirm-delete mode = %q, want it to mention the delete target", got)
	}
}

func TestProjectsViewForm_TitlesByMode(t *testing.T) {
	newM := ProjectsModel{mode: projectsModeNewProject, inputs: makeProjectInputs(nil, ""), width: 100}
	if got := newM.viewForm(); !strings.Contains(got, "New Project") {
		t.Errorf("viewForm() in new mode = %q, want it to contain %q", got, "New Project")
	}

	editM := ProjectsModel{mode: projectsModeEditProject, inputs: makeProjectInputs(nil, ""), width: 100}
	if got := editM.viewForm(); !strings.Contains(got, "Edit Project") {
		t.Errorf("viewForm() in edit mode = %q, want it to contain %q", got, "Edit Project")
	}
}

func TestProjectsViewForm_DropdownRenders(t *testing.T) {
	clients := []*model.Client{{ID: 1, Name: "Acme"}, {ID: 2, Name: "Beta"}}
	inputs := makeProjectInputs(nil, "")
	m := ProjectsModel{
		mode: projectsModeNewProject, width: 100,
		clients: clients, inputs: inputs,
		focusedInput: projFieldClient, showClientDrop: true,
	}
	m.rebuildClientMatches()

	got := m.viewForm()
	if !strings.Contains(got, "Acme") || !strings.Contains(got, "Beta") {
		t.Errorf("viewForm() with dropdown open = %q, want both client names listed", got)
	}
}

func TestProjectsViewForm_ErrorLine(t *testing.T) {
	m := ProjectsModel{mode: projectsModeNewProject, inputs: makeProjectInputs(nil, ""), width: 100, err: "bad input"}
	got := m.viewForm()
	if !strings.Contains(got, "bad input") {
		t.Errorf("viewForm() = %q, want the error message rendered", got)
	}
}

// ── renderField (exercised through viewForm for both focus states) ───────────

func TestProjectsRenderField_FocusedAndUnfocused(t *testing.T) {
	inputs := makeProjectInputs(nil, "")
	inputs[projFieldClient].SetValue("Acme")
	inputs[projFieldName].SetValue("Website")
	m := ProjectsModel{mode: projectsModeNewProject, inputs: inputs, focusedInput: projFieldClient, width: 100}

	got := m.viewForm()
	if !strings.Contains(got, "Acme") || !strings.Contains(got, "Website") {
		t.Errorf("viewForm() = %q, want both field values rendered regardless of focus", got)
	}
}
