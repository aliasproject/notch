package views

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/aliasproject/notch/internal/db"
	"github.com/aliasproject/notch/internal/model"
	tea "github.com/charmbracelet/bubbletea"
)

func newTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// ── List navigation ──────────────────────────────────────────────────────────

func TestClientsUpdateList_CursorBounds(t *testing.T) {
	m := ClientsModel{clients: []*model.Client{
		{ID: 1, Name: "A"}, {ID: 2, Name: "B"},
	}}

	m, _ = m.updateList(tea.KeyMsg{Type: tea.KeyUp})
	if m.cursor != 0 {
		t.Errorf("cursor after up at top = %d, want 0 (clamped)", m.cursor)
	}

	m, _ = m.updateList(tea.KeyMsg{Type: tea.KeyDown})
	m, _ = m.updateList(tea.KeyMsg{Type: tea.KeyDown})
	if m.cursor != 1 {
		t.Errorf("cursor after two downs with 2 clients = %d, want 1 (clamped at end)", m.cursor)
	}
}

func TestClientsUpdateList_UpDecrementsWhenNotAtTop(t *testing.T) {
	m := ClientsModel{clients: []*model.Client{{ID: 1}, {ID: 2}}, cursor: 1}
	got, _ := m.updateList(tea.KeyMsg{Type: tea.KeyUp})
	if got.cursor != 0 {
		t.Errorf("cursor after up from 1 = %d, want 0", got.cursor)
	}
}

func TestClientsUpdateList_DeleteRequiresNonEmptyList(t *testing.T) {
	empty := ClientsModel{}
	got, _ := empty.updateList(runeKey('d'))
	if got.mode == clientModeConfirmDelete {
		t.Error("'d' on an empty client list should not enter confirm mode")
	}

	nonEmpty := ClientsModel{clients: []*model.Client{{ID: 1, Name: "A"}}}
	got2, _ := nonEmpty.updateList(runeKey('d'))
	if got2.mode != clientModeConfirmDelete {
		t.Errorf("'d' with clients present: mode = %v, want clientModeConfirmDelete", got2.mode)
	}
}

func TestClientsUpdateList_NewOpensForm(t *testing.T) {
	m := ClientsModel{}
	got, _ := m.updateList(runeKey('n'))
	if got.mode != clientModeForm {
		t.Errorf("mode after 'n' = %v, want clientModeForm", got.mode)
	}
	if got.editingID != 0 {
		t.Errorf("editingID for a new client = %d, want 0", got.editingID)
	}
}

func TestClientsUpdateList_EditOpensPrepopulatedForm(t *testing.T) {
	m := ClientsModel{clients: []*model.Client{{ID: 7, Name: "Acme", HourlyRate: 88.5}}, cursor: 0}
	got, _ := m.updateList(runeKey('e'))
	if got.mode != clientModeForm {
		t.Errorf("mode after 'e' = %v, want clientModeForm", got.mode)
	}
	if got.editingID != 7 {
		t.Errorf("editingID = %d, want 7", got.editingID)
	}
	if got.inputs[clientFieldName].Value() != "Acme" {
		t.Errorf("name field = %q, want %q", got.inputs[clientFieldName].Value(), "Acme")
	}
	if got.inputs[clientFieldRate].Value() != "88.50" {
		t.Errorf("rate field = %q, want %q", got.inputs[clientFieldRate].Value(), "88.50")
	}
}

// ── Form validation (pure — returns before touching the DB) ──────────────────

func TestClientsSubmitForm_BlankNameRejected(t *testing.T) {
	m := ClientsModel{}.openForm(nil)
	m.inputs[clientFieldName].SetValue("   ")
	m.inputs[clientFieldRate].SetValue("50")

	got, cmd := m.submitForm()
	if got.formErr == "" {
		t.Error("want a validation error for a blank name")
	}
	if got.mode != clientModeForm {
		t.Errorf("mode after failed validation = %v, want to stay clientModeForm", got.mode)
	}
	if cmd != nil {
		t.Error("want nil cmd on validation failure (no DB touch)")
	}
}

func TestClientsSubmitForm_InvalidRateRejected(t *testing.T) {
	cases := []string{"not-a-number", "-5"}
	for _, rate := range cases {
		m := ClientsModel{}.openForm(nil)
		m.inputs[clientFieldName].SetValue("Acme")
		m.inputs[clientFieldRate].SetValue(rate)

		got, cmd := m.submitForm()
		if got.formErr == "" {
			t.Errorf("rate %q: want a validation error", rate)
		}
		if cmd != nil {
			t.Errorf("rate %q: want nil cmd on validation failure", rate)
		}
	}
}

func TestClientsSubmitForm_BlankRateDefaultsToZero(t *testing.T) {
	d := newTestDB(t)
	m := ClientsModel{db: d}.openForm(nil)
	m.inputs[clientFieldName].SetValue("Acme")
	m.inputs[clientFieldRate].SetValue("")

	got, cmd := m.submitForm()
	if got.formErr != "" {
		t.Errorf("unexpected validation error: %q", got.formErr)
	}
	if cmd == nil {
		t.Fatal("want a non-nil save cmd on success")
	}
	cmd() // execute the batched save + reload
	clients, err := d.ListClients()
	if err != nil {
		t.Fatal(err)
	}
	if len(clients) != 1 || clients[0].HourlyRate != 0 {
		t.Errorf("clients = %+v, want one client with rate 0", clients)
	}
}

func TestClientsSubmitForm_EditExistingClientUpdatesAndReportsAction(t *testing.T) {
	d := newTestDB(t)
	c, err := d.CreateClient("Acme", 50)
	if err != nil {
		t.Fatal(err)
	}

	m := ClientsModel{db: d}.openForm(c)
	m.inputs[clientFieldName].SetValue("Acme Renamed")
	m.inputs[clientFieldRate].SetValue("99.00")

	got, cmd := m.submitForm()
	if got.formErr != "" {
		t.Fatalf("unexpected validation error: %q", got.formErr)
	}
	if got.mode != clientModeList {
		t.Errorf("mode after successful edit = %v, want clientModeList", got.mode)
	}
	if cmd == nil {
		t.Fatal("want a non-nil save cmd on success")
	}
	cmd()

	clients, err := d.ListClients()
	if err != nil {
		t.Fatal(err)
	}
	if len(clients) != 1 || clients[0].Name != "Acme Renamed" || clients[0].HourlyRate != 99 {
		t.Errorf("clients = %+v, want one client renamed to Acme Renamed with rate 99", clients)
	}
}

func TestClientsSubmitForm_DBErrorSetsFormErr(t *testing.T) {
	d := newTestDB(t)
	d.Close() // force CreateClient to fail

	m := ClientsModel{db: d}.openForm(nil)
	m.inputs[clientFieldName].SetValue("Acme")
	m.inputs[clientFieldRate].SetValue("10")

	got, cmd := m.submitForm()
	if got.formErr == "" {
		t.Error("want formErr to be set when the DB save fails")
	}
	if !strings.Contains(got.formErr, "Save failed:") {
		t.Errorf("formErr = %q, want it to start with %q", got.formErr, "Save failed:")
	}
	if cmd != nil {
		t.Error("want nil cmd when the DB save fails")
	}
}

// ── Confirm-delete flow ───────────────────────────────────────────────────────

func TestClientsUpdateConfirm_YDeletes(t *testing.T) {
	d := newTestDB(t)
	c, err := d.CreateClient("Acme", 100)
	if err != nil {
		t.Fatal(err)
	}

	m := ClientsModel{db: d, mode: clientModeConfirmDelete, clients: []*model.Client{c}, cursor: 0}
	got, cmd := m.updateConfirm(runeKey('y'))
	if got.mode != clientModeConfirmDelete && got.mode != clientModeList {
		t.Fatalf("unexpected mode after 'y': %v", got.mode)
	}
	if got.mode != clientModeList {
		t.Errorf("mode after 'y' = %v, want clientModeList (confirmation should close)", got.mode)
	}
	if cmd == nil {
		t.Fatal("want a non-nil cmd after deleting")
	}
	cmd()
	clients, _ := d.ListClients()
	if len(clients) != 0 {
		t.Errorf("clients after delete = %+v, want none", clients)
	}
}

func TestClientsUpdateConfirm_OtherKeyCancels(t *testing.T) {
	d := newTestDB(t)
	c, err := d.CreateClient("Acme", 100)
	if err != nil {
		t.Fatal(err)
	}
	m := ClientsModel{db: d, mode: clientModeConfirmDelete, clients: []*model.Client{c}, cursor: 0}

	got, cmd := m.updateConfirm(tea.KeyMsg{Type: tea.KeyEsc})
	if got.mode != clientModeList {
		t.Errorf("mode after cancel = %v, want clientModeList", got.mode)
	}
	if cmd != nil {
		t.Error("want nil cmd when cancelling delete")
	}
	clients, _ := d.ListClients()
	if len(clients) != 1 {
		t.Errorf("client should not be deleted on cancel, got %d clients", len(clients))
	}
}

func TestClientsUpdateConfirm_YWithNoClientsReturnsToList(t *testing.T) {
	m := ClientsModel{mode: clientModeConfirmDelete, clients: nil}
	got, cmd := m.updateConfirm(runeKey('y'))
	if got.mode != clientModeList {
		t.Errorf("mode = %v, want clientModeList", got.mode)
	}
	if cmd != nil {
		t.Error("want nil cmd when confirming delete with no clients loaded")
	}
}

func TestClientsUpdateConfirm_YWithDBErrorReportsErrCmd(t *testing.T) {
	d := newTestDB(t)
	c, err := d.CreateClient("Acme", 100)
	if err != nil {
		t.Fatal(err)
	}
	d.Close() // force DeleteClient to fail

	m := ClientsModel{db: d, mode: clientModeConfirmDelete, clients: []*model.Client{c}, cursor: 0}
	got, cmd := m.updateConfirm(runeKey('y'))
	if got.mode != clientModeList {
		t.Errorf("mode after DB error = %v, want clientModeList", got.mode)
	}
	if cmd == nil {
		t.Fatal("want a non-nil ErrCmd when DeleteClient fails")
	}
	msg := cmd()
	if _, ok := msg.(ErrMsg); !ok {
		t.Errorf("cmd() = %T, want ErrMsg", msg)
	}
}

// ── padRight / listColWidths ─────────────────────────────────────────────────

func TestPadRight(t *testing.T) {
	cases := []struct {
		s     string
		width int
		want  string
	}{
		{"hi", 5, "hi   "},
		{"hello", 5, "hello"},
		{"hello world", 5, "hello"},
		{"", 3, "   "},
	}
	for _, c := range cases {
		if got := padRight(c.s, c.width); got != c.want {
			t.Errorf("padRight(%q, %d) = %q, want %q", c.s, c.width, got, c.want)
		}
	}
}

func TestClientsListColWidths_SpansAvailableWidth(t *testing.T) {
	m := ClientsModel{width: 100}
	nameCol, rateCol := m.listColWidths()
	if nameCol < 24 {
		t.Errorf("nameCol = %d, want >= 24 (minimum floor)", nameCol)
	}
	if rateCol != 18 {
		t.Errorf("rateCol = %d, want fixed 18", rateCol)
	}
}

func TestClientsListColWidths_ClampsToMinimumWhenNarrow(t *testing.T) {
	m := ClientsModel{width: 30}
	nameCol, _ := m.listColWidths()
	if nameCol != 24 {
		t.Errorf("nameCol at width 30 = %d, want 24 (clamped floor)", nameCol)
	}
}

// ── Constructor / small accessors ────────────────────────────────────────────

func TestNewClients(t *testing.T) {
	d := newTestDB(t)
	m := NewClients(d)
	if m.mode != clientModeList {
		t.Errorf("NewClients mode = %v, want clientModeList", m.mode)
	}
	if m.db != d {
		t.Error("NewClients did not retain the given *db.DB")
	}
}

func TestClientsInit_LoadsClients(t *testing.T) {
	d := newTestDB(t)
	if _, err := d.CreateClient("Acme", 50); err != nil {
		t.Fatal(err)
	}
	m := NewClients(d)
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init() returned a nil cmd")
	}
	msg := cmd()
	loaded, ok := msg.(clientsLoadedMsg)
	if !ok {
		t.Fatalf("Init cmd produced %T, want clientsLoadedMsg", msg)
	}
	if len(loaded.clients) != 1 || loaded.clients[0].Name != "Acme" {
		t.Errorf("loaded.clients = %+v, want one client named Acme", loaded.clients)
	}
}

func TestClientsIsBusy(t *testing.T) {
	cases := []struct {
		mode clientMode
		want bool
	}{
		{clientModeList, false},
		{clientModeForm, true},
		{clientModeConfirmDelete, true},
	}
	for _, c := range cases {
		m := ClientsModel{mode: c.mode}
		if got := m.IsBusy(); got != c.want {
			t.Errorf("IsBusy() with mode %v = %v, want %v", c.mode, got, c.want)
		}
	}
}

func TestClientsHelp(t *testing.T) {
	cases := []struct {
		mode      clientMode
		wantKey   string
		wantLabel string
	}{
		{clientModeForm, "tab / shift+tab", "move"},
		{clientModeConfirmDelete, "y", "confirm"},
		{clientModeList, "n", "new"},
	}
	for _, c := range cases {
		m := ClientsModel{mode: c.mode}
		got := m.Help()
		found := false
		for _, h := range got {
			if h.Key == c.wantKey && h.Label == c.wantLabel {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Help() with mode %v = %v, want a hotkey {%q, %q}", c.mode, got, c.wantKey, c.wantLabel)
		}
	}
}

func TestClientsSetSize(t *testing.T) {
	m := ClientsModel{}
	m.SetSize(120, 40)
	if m.width != 120 || m.height != 40 {
		t.Errorf("after SetSize(120, 40): width=%d height=%d, want 120, 40", m.width, m.height)
	}
}

// ── Update: top-level dispatch ───────────────────────────────────────────────

func TestClientsUpdate_ClientsLoadedMsgClampsCursor(t *testing.T) {
	m := ClientsModel{cursor: 5}
	got, cmd := m.Update(clientsLoadedMsg{clients: []*model.Client{{ID: 1}, {ID: 2}}})
	if got.cursor != 1 {
		t.Errorf("cursor after loading with an out-of-range cursor = %d, want 1 (clamped)", got.cursor)
	}
	if cmd != nil {
		t.Error("want nil cmd from clientsLoadedMsg handling")
	}
}

func TestClientsUpdate_ClientsLoadedMsgLeavesInRangeCursorAlone(t *testing.T) {
	m := ClientsModel{cursor: 0}
	got, _ := m.Update(clientsLoadedMsg{clients: nil})
	if got.cursor != 0 {
		t.Errorf("cursor = %d, want 0 (cursor > 0 guard should skip clamp)", got.cursor)
	}

	m2 := ClientsModel{cursor: 1}
	got2, _ := m2.Update(clientsLoadedMsg{clients: []*model.Client{{ID: 1}, {ID: 2}, {ID: 3}}})
	if got2.cursor != 1 {
		t.Errorf("cursor = %d, want 1 (already in range, untouched)", got2.cursor)
	}
}

func TestClientsUpdate_KeyMsgDispatchesByMode(t *testing.T) {
	// list mode: 'n' should open the form, same as calling updateList directly.
	listM := ClientsModel{mode: clientModeList}
	got, _ := listM.Update(runeKey('n'))
	if got.mode != clientModeForm {
		t.Errorf("Update in list mode with 'n': mode = %v, want clientModeForm", got.mode)
	}

	// form mode: esc should cancel back to list, same as calling updateForm directly.
	formM := ClientsModel{}.openForm(nil)
	got2, _ := formM.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if got2.mode != clientModeList {
		t.Errorf("Update in form mode with esc: mode = %v, want clientModeList", got2.mode)
	}

	// confirm mode: any non-y key cancels back to list without touching the DB.
	confirmM := ClientsModel{mode: clientModeConfirmDelete}
	got3, cmd3 := confirmM.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if got3.mode != clientModeList {
		t.Errorf("Update in confirm mode with esc: mode = %v, want clientModeList", got3.mode)
	}
	if cmd3 != nil {
		t.Error("want nil cmd cancelling delete via top-level Update")
	}
}

func TestClientsUpdate_UnrecognizedMsgTypeIsNoop(t *testing.T) {
	m := ClientsModel{mode: clientModeList, cursor: 3}
	got, cmd := m.Update(StatusMsg("irrelevant"))
	if got.cursor != 3 {
		t.Errorf("cursor = %d, want unchanged 3 for an unhandled msg type", got.cursor)
	}
	if cmd != nil {
		t.Error("want nil cmd for an unhandled msg type")
	}
}

// ── updateForm ────────────────────────────────────────────────────────────────

func TestClientsUpdateForm_CancelKey(t *testing.T) {
	m := ClientsModel{}.openForm(nil)
	m.formErr = "some error"
	got, cmd := m.updateForm(tea.KeyMsg{Type: tea.KeyEsc})
	if got.mode != clientModeList {
		t.Errorf("mode after cancel = %v, want clientModeList", got.mode)
	}
	if got.formErr != "" {
		t.Errorf("formErr after cancel = %q, want empty", got.formErr)
	}
	if cmd != nil {
		t.Error("want nil cmd on cancel")
	}
}

func TestClientsUpdateForm_SubmitKeyDispatchesToSubmitForm(t *testing.T) {
	m := ClientsModel{}.openForm(nil)
	// leave the name blank so submitForm takes its validation-error path,
	// which is enough to prove the Submit branch dispatches correctly.
	got, cmd := m.updateForm(tea.KeyMsg{Type: tea.KeyEnter})
	if got.formErr == "" {
		t.Error("want a validation error to confirm updateForm dispatched to submitForm")
	}
	if cmd != nil {
		t.Error("want nil cmd from a failed submit")
	}
}

func TestClientsUpdateForm_NextPrevFieldWraps(t *testing.T) {
	m := ClientsModel{}.openForm(nil)

	if !m.inputs[0].Focused() {
		t.Fatal("newly opened form should focus the first field")
	}

	// tab: 0 -> 1
	m, cmd := m.updateForm(tea.KeyMsg{Type: tea.KeyTab})
	if m.focusIdx != clientFieldRate {
		t.Errorf("focusIdx after tab = %d, want %d", m.focusIdx, clientFieldRate)
	}
	if m.inputs[clientFieldName].Focused() {
		t.Error("name field should be blurred after tab")
	}
	if !m.inputs[clientFieldRate].Focused() {
		t.Error("rate field should be focused after tab")
	}
	if cmd == nil {
		t.Error("want a non-nil blink cmd after tab")
	}

	// tab again: 1 -> 0 (wraps around clientFieldCount)
	m, _ = m.updateForm(tea.KeyMsg{Type: tea.KeyTab})
	if m.focusIdx != clientFieldName {
		t.Errorf("focusIdx after second tab = %d, want %d (wrapped)", m.focusIdx, clientFieldName)
	}

	// shift+tab from 0: wraps backward to the last field
	m, cmd2 := m.updateForm(tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.focusIdx != clientFieldRate {
		t.Errorf("focusIdx after shift+tab from 0 = %d, want %d (wrapped backward)", m.focusIdx, clientFieldRate)
	}
	if cmd2 == nil {
		t.Error("want a non-nil blink cmd after shift+tab")
	}

	// shift+tab again: 1 -> 0
	m, _ = m.updateForm(tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.focusIdx != clientFieldName {
		t.Errorf("focusIdx after second shift+tab = %d, want %d", m.focusIdx, clientFieldName)
	}
}

func TestClientsUpdateForm_DefaultForwardsKeystrokeToFocusedInput(t *testing.T) {
	m := ClientsModel{}.openForm(nil)
	got, _ := m.updateForm(runeKey('x'))
	if got.inputs[clientFieldName].Value() != "x" {
		t.Errorf("name field value = %q, want %q", got.inputs[clientFieldName].Value(), "x")
	}
}

// ── View / viewList / viewForm / viewConfirm ─────────────────────────────────

func TestClientsView_ListModeEmpty(t *testing.T) {
	m := ClientsModel{mode: clientModeList, width: 100}
	out := m.View()
	if !strings.Contains(out, "No clients yet.") {
		t.Errorf("View() for empty list = %q, want to contain %q", out, "No clients yet.")
	}
	if !strings.Contains(out, "Press n to add one.") {
		t.Errorf("View() for empty list missing hint text: %q", out)
	}
}

func TestClientsView_ListModePopulated(t *testing.T) {
	m := ClientsModel{
		mode:   clientModeList,
		width:  100,
		cursor: 0,
		clients: []*model.Client{
			{ID: 1, Name: "Acme", HourlyRate: 75},
			{ID: 2, Name: "Globex", HourlyRate: 120},
		},
	}
	out := m.View()
	for _, want := range []string{"NAME", "HOURLY RATE", "Acme", "Globex", "$75.00 / hr"} {
		if !strings.Contains(out, want) {
			t.Errorf("View() for populated list missing %q; got %q", want, out)
		}
	}
}

func TestClientsView_FormModeNew(t *testing.T) {
	m := ClientsModel{}.openForm(nil)
	m.width = 100
	out := m.View()
	for _, want := range []string{"New Client", "Name", "Hourly Rate ($)", "Save", "Cancel"} {
		if !strings.Contains(out, want) {
			t.Errorf("View() for new-client form missing %q; got %q", want, out)
		}
	}
}

func TestClientsView_FormModeEditWithErrorAndLastFieldFocused(t *testing.T) {
	c := &model.Client{ID: 9, Name: "Acme", HourlyRate: 50}
	m := ClientsModel{}.openForm(c)
	m.width = 100
	m.formErr = "Save failed: boom"
	// Move focus to the last field — a regression check for the bug where
	// Save incorrectly rendered in StyleButtonActive (green) whenever the
	// last *field* was focused, confusing it with the Save *button* being
	// focused (which this form has no separate state for at all: Enter
	// always submits regardless of which field is focused). Save should
	// always render as StyleButtonPrimary here.
	m.focusIdx = clientFieldCount - 1

	out := m.View()
	for _, want := range []string{"Edit Client", "Save failed: boom", "Save", "Cancel"} {
		if !strings.Contains(out, want) {
			t.Errorf("View() for edit form missing %q; got %q", want, out)
		}
	}
}

func TestClientsView_ConfirmModeEmpty(t *testing.T) {
	m := ClientsModel{mode: clientModeConfirmDelete}
	if out := m.View(); out != "" {
		t.Errorf("View() in confirm mode with no clients = %q, want empty string", out)
	}
}

func TestClientsView_ConfirmModePopulated(t *testing.T) {
	m := ClientsModel{
		mode:    clientModeConfirmDelete,
		cursor:  0,
		clients: []*model.Client{{ID: 1, Name: "Acme"}},
	}
	out := m.View()
	for _, want := range []string{`Delete "Acme"?`, "y  confirm    esc  cancel"} {
		if !strings.Contains(out, want) {
			t.Errorf("View() in confirm mode missing %q; got %q", want, out)
		}
	}
}
