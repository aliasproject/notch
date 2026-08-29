package db_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aliasproject/notch/internal/db"
	"github.com/aliasproject/notch/internal/model"
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

// ── Clients ──────────────────────────────────────────────────────────────────

func TestClientCRUD(t *testing.T) {
	d := newTestDB(t)

	c, err := d.CreateClient("Acme", 100)
	if err != nil {
		t.Fatalf("CreateClient: %v", err)
	}
	if c.ID == 0 {
		t.Fatal("CreateClient returned zero ID")
	}

	got, err := d.GetClient(c.ID)
	if err != nil {
		t.Fatalf("GetClient: %v", err)
	}
	if got.Name != "Acme" || got.HourlyRate != 100 {
		t.Errorf("GetClient = %+v, want Name=Acme HourlyRate=100", got)
	}

	c.Name = "Acme Corp"
	c.HourlyRate = 150
	if err := d.UpdateClient(c); err != nil {
		t.Fatalf("UpdateClient: %v", err)
	}
	got, _ = d.GetClient(c.ID)
	if got.Name != "Acme Corp" || got.HourlyRate != 150 {
		t.Errorf("after update, GetClient = %+v, want Name=Acme Corp HourlyRate=150", got)
	}

	if err := d.DeleteClient(c.ID); err != nil {
		t.Fatalf("DeleteClient: %v", err)
	}
	if _, err := d.GetClient(c.ID); err == nil {
		t.Error("GetClient after delete: want error, got nil")
	}
}

func TestListClients_SortedByName(t *testing.T) {
	d := newTestDB(t)
	for _, name := range []string{"Zeta", "Alpha", "Mid"} {
		if _, err := d.CreateClient(name, 0); err != nil {
			t.Fatalf("CreateClient(%s): %v", name, err)
		}
	}

	clients, err := d.ListClients()
	if err != nil {
		t.Fatalf("ListClients: %v", err)
	}
	if len(clients) != 3 {
		t.Fatalf("len(clients) = %d, want 3", len(clients))
	}
	want := []string{"Alpha", "Mid", "Zeta"}
	for i, name := range want {
		if clients[i].Name != name {
			t.Errorf("clients[%d].Name = %q, want %q", i, clients[i].Name, name)
		}
	}
}

func TestCreateClient_DuplicateNameRejected(t *testing.T) {
	d := newTestDB(t)
	if _, err := d.CreateClient("Acme", 0); err != nil {
		t.Fatalf("CreateClient: %v", err)
	}
	if _, err := d.CreateClient("Acme", 0); err == nil {
		t.Error("CreateClient with duplicate name: want error, got nil")
	}
}

// ── Projects ─────────────────────────────────────────────────────────────────

func mustClient(t *testing.T, d *db.DB, name string) *model.Client {
	t.Helper()
	c, err := d.CreateClient(name, 100)
	if err != nil {
		t.Fatalf("CreateClient(%s): %v", name, err)
	}
	return c
}

func TestProjectCRUD(t *testing.T) {
	d := newTestDB(t)
	c := mustClient(t, d, "Acme")

	p, err := d.CreateProject(c.ID, "Website")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	got, err := d.GetProject(p.ID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.Name != "Website" || got.ClientID != c.ID || got.Client.Name != "Acme" {
		t.Errorf("GetProject = %+v, want Name=Website ClientID=%d Client.Name=Acme", got, c.ID)
	}

	p.Name = "Redesign"
	if err := d.UpdateProject(p); err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	got, _ = d.GetProject(p.ID)
	if got.Name != "Redesign" {
		t.Errorf("after update, GetProject.Name = %q, want Redesign", got.Name)
	}

	if err := d.DeleteProject(p.ID); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	if _, err := d.GetProject(p.ID); err == nil {
		t.Error("GetProject after delete: want error, got nil")
	}
}

func TestListProjects_AllAndScoped(t *testing.T) {
	d := newTestDB(t)
	c1 := mustClient(t, d, "Alpha")
	c2 := mustClient(t, d, "Beta")
	if _, err := d.CreateProject(c1.ID, "P1"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateProject(c1.ID, "P2"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateProject(c2.ID, "P3"); err != nil {
		t.Fatal(err)
	}

	all, err := d.ListProjects(0)
	if err != nil {
		t.Fatalf("ListProjects(0): %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("len(all) = %d, want 3", len(all))
	}

	scoped, err := d.ListProjects(c1.ID)
	if err != nil {
		t.Fatalf("ListProjects(c1): %v", err)
	}
	if len(scoped) != 2 {
		t.Fatalf("len(scoped) = %d, want 2", len(scoped))
	}
	for _, p := range scoped {
		if p.ClientID != c1.ID {
			t.Errorf("scoped project %+v has ClientID != c1.ID", p)
		}
	}
}

func TestCreateProject_DuplicateNameWithinClientRejected(t *testing.T) {
	d := newTestDB(t)
	c := mustClient(t, d, "Acme")
	if _, err := d.CreateProject(c.ID, "Website"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateProject(c.ID, "Website"); err == nil {
		t.Error("CreateProject with duplicate (client,name): want error, got nil")
	}
	// Same name under a different client is fine.
	c2 := mustClient(t, d, "Other")
	if _, err := d.CreateProject(c2.ID, "Website"); err != nil {
		t.Errorf("CreateProject with same name under different client: %v", err)
	}
}

func TestDeleteClient_CascadesToProjectsAndEntries(t *testing.T) {
	d := newTestDB(t)
	c := mustClient(t, d, "Acme")
	p, err := d.CreateProject(c.ID, "Website")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.StartEntry(p.ID, "build"); err != nil {
		t.Fatal(err)
	}

	if err := d.DeleteClient(c.ID); err != nil {
		t.Fatalf("DeleteClient: %v", err)
	}

	if _, err := d.GetProject(p.ID); err == nil {
		t.Error("project should have been cascade-deleted with its client")
	}
	entries, err := d.ListEntries(0, "", "", true)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("entries after cascading client delete = %d, want 0", len(entries))
	}
}

// ── Entries ──────────────────────────────────────────────────────────────────

func mustProject(t *testing.T, d *db.DB) *model.Project {
	t.Helper()
	c := mustClient(t, d, "Acme")
	p, err := d.CreateProject(c.ID, "Website")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return p
}

func TestEntryLifecycle(t *testing.T) {
	d := newTestDB(t)
	p := mustProject(t, d)

	e, err := d.StartEntry(p.ID, "build feature")
	if err != nil {
		t.Fatalf("StartEntry: %v", err)
	}
	if e.EndTime != nil {
		t.Error("freshly started entry should have nil EndTime")
	}

	running, err := d.GetRunningEntry()
	if err != nil {
		t.Fatalf("GetRunningEntry: %v", err)
	}
	if running == nil || running.ID != e.ID {
		t.Errorf("GetRunningEntry = %+v, want entry with ID %d", running, e.ID)
	}

	if err := d.StopEntry(e.ID); err != nil {
		t.Fatalf("StopEntry: %v", err)
	}
	running, err = d.GetRunningEntry()
	if err != nil {
		t.Fatalf("GetRunningEntry after stop: %v", err)
	}
	if running != nil {
		t.Errorf("GetRunningEntry after stop = %+v, want nil", running)
	}

	entries, err := d.ListEntries(0, "", "", true)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].EndTime == nil {
		t.Fatalf("ListEntries = %+v, want 1 entry with EndTime set", entries)
	}

	if err := d.DeleteEntry(e.ID); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}
	entries, _ = d.ListEntries(0, "", "", true)
	if len(entries) != 0 {
		t.Errorf("entries after delete = %d, want 0", len(entries))
	}
}

func TestStopAllRunning(t *testing.T) {
	d := newTestDB(t)
	p := mustProject(t, d)

	e1, err := d.StartEntry(p.ID, "task1")
	if err != nil {
		t.Fatal(err)
	}
	e2, err := d.StartEntry(p.ID, "task2")
	if err != nil {
		t.Fatal(err)
	}

	if err := d.StopAllRunning(); err != nil {
		t.Fatalf("StopAllRunning: %v", err)
	}

	entries, err := d.ListEntries(0, "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[int64]*model.Entry{}
	for _, e := range entries {
		byID[e.ID] = e
	}
	for _, id := range []int64{e1.ID, e2.ID} {
		if e := byID[id]; e == nil || e.EndTime == nil {
			t.Errorf("entry %d should be stopped after StopAllRunning", id)
		}
	}
}

func TestUpdateEntry(t *testing.T) {
	d := newTestDB(t)
	p := mustProject(t, d)
	e, err := d.StartEntry(p.ID, "original task")
	if err != nil {
		t.Fatal(err)
	}

	start := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	end := start.Add(3 * time.Hour)
	upd := &model.Entry{
		ID:        e.ID,
		ProjectID: p.ID,
		Task:      "renamed task",
		StartTime: start,
		EndTime:   &end,
		Invoiced:  true,
		Paid:      false,
		Notes:     "some notes",
	}
	if err := d.UpdateEntry(upd); err != nil {
		t.Fatalf("UpdateEntry: %v", err)
	}

	entries, err := d.ListEntries(0, "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	got := entries[0]
	if got.Task != "renamed task" || !got.Invoiced || got.Paid || got.Notes != "some notes" {
		t.Errorf("UpdateEntry did not persist: %+v", got)
	}
	if !got.StartTime.Equal(start) {
		t.Errorf("StartTime = %v, want %v", got.StartTime, start)
	}
	if got.EndTime == nil || !got.EndTime.Equal(end) {
		t.Errorf("EndTime = %v, want %v", got.EndTime, end)
	}
}

func TestDeleteProject_CascadesToEntries(t *testing.T) {
	d := newTestDB(t)
	p := mustProject(t, d)
	if _, err := d.StartEntry(p.ID, "task"); err != nil {
		t.Fatal(err)
	}

	if err := d.DeleteProject(p.ID); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}

	entries, err := d.ListEntries(0, "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("entries after cascading project delete = %d, want 0", len(entries))
	}
}

// forceStartTime overwrites an entry's start_time (and clears end_time) via
// UpdateEntry so tests can construct exact timestamp ties that StartEntry's
// wall-clock resolution wouldn't reliably reproduce.
func forceStartTime(t *testing.T, d *db.DB, e *model.Entry, ts time.Time) {
	t.Helper()
	if err := d.UpdateEntry(&model.Entry{
		ID:        e.ID,
		ProjectID: e.ProjectID,
		Task:      e.Task,
		StartTime: ts,
		EndTime:   nil,
	}); err != nil {
		t.Fatalf("forceStartTime: %v", err)
	}
}

func TestGetRunningEntry_TieBreaksByHighestID(t *testing.T) {
	d := newTestDB(t)
	p := mustProject(t, d)

	e1, err := d.StartEntry(p.ID, "first")
	if err != nil {
		t.Fatal(err)
	}
	e2, err := d.StartEntry(p.ID, "second")
	if err != nil {
		t.Fatal(err)
	}
	if e2.ID <= e1.ID {
		t.Fatalf("expected e2.ID > e1.ID, got e1=%d e2=%d", e1.ID, e2.ID)
	}

	same := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	forceStartTime(t, d, e1, same)
	forceStartTime(t, d, e2, same)

	running, err := d.GetRunningEntry()
	if err != nil {
		t.Fatalf("GetRunningEntry: %v", err)
	}
	if running == nil || running.ID != e2.ID {
		t.Errorf("GetRunningEntry = %+v, want the higher-id entry (%d) on a start_time tie", running, e2.ID)
	}
}

func TestListEntries_TieBreaksByHighestID(t *testing.T) {
	d := newTestDB(t)
	p := mustProject(t, d)

	e1, err := d.StartEntry(p.ID, "first")
	if err != nil {
		t.Fatal(err)
	}
	e2, err := d.StartEntry(p.ID, "second")
	if err != nil {
		t.Fatal(err)
	}

	same := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	forceStartTime(t, d, e1, same)
	forceStartTime(t, d, e2, same)

	entries, err := d.ListEntries(0, "", "", true)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].ID != e2.ID || entries[1].ID != e1.ID {
		t.Errorf("ListEntries order = [%d, %d], want [%d, %d] (higher id first on a tie)",
			entries[0].ID, entries[1].ID, e2.ID, e1.ID)
	}
}

func TestListEntries_Filters(t *testing.T) {
	d := newTestDB(t)
	c := mustClient(t, d, "Acme")
	p1, err := d.CreateProject(c.ID, "P1")
	if err != nil {
		t.Fatal(err)
	}
	p2, err := d.CreateProject(c.ID, "P2")
	if err != nil {
		t.Fatal(err)
	}

	// p1: one finished entry on 2026-01-05, one running entry (no end_time).
	e1, err := d.StartEntry(p1.ID, "a")
	if err != nil {
		t.Fatal(err)
	}
	forceStartTime(t, d, e1, time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC))
	if err := d.UpdateEntry(&model.Entry{
		ID: e1.ID, ProjectID: p1.ID, Task: "a",
		StartTime: time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC),
		EndTime:   ptrTime(time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)),
	}); err != nil {
		t.Fatal(err)
	}

	e2, err := d.StartEntry(p1.ID, "running-on-p1")
	if err != nil {
		t.Fatal(err)
	}
	forceStartTime(t, d, e2, time.Date(2026, 1, 10, 9, 0, 0, 0, time.UTC))

	// p2: one finished entry on 2026-02-01.
	e3, err := d.StartEntry(p2.ID, "b")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.UpdateEntry(&model.Entry{
		ID: e3.ID, ProjectID: p2.ID, Task: "b",
		StartTime: time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC),
		EndTime:   ptrTime(time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC)),
	}); err != nil {
		t.Fatal(err)
	}

	t.Run("by project", func(t *testing.T) {
		entries, err := d.ListEntries(p1.ID, "", "", true)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 2 {
			t.Fatalf("len(entries) = %d, want 2", len(entries))
		}
		for _, e := range entries {
			if e.ProjectID != p1.ID {
				t.Errorf("entry %d has ProjectID %d, want %d", e.ID, e.ProjectID, p1.ID)
			}
		}
	})

	t.Run("by date range", func(t *testing.T) {
		entries, err := d.ListEntries(0, "2026-01-01", "2026-01-31", true)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 2 {
			t.Fatalf("len(entries) = %d, want 2 (both January entries)", len(entries))
		}
	})

	t.Run("excludeRunning", func(t *testing.T) {
		entries, err := d.ListEntries(0, "", "", false)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.IsRunning() {
				t.Errorf("entry %d is running but includeRunning=false was requested", e.ID)
			}
		}
		if len(entries) != 2 {
			t.Fatalf("len(entries) = %d, want 2 (the two finished entries)", len(entries))
		}
	})
}

func ptrTime(t time.Time) *time.Time { return &t }

func TestSetEntryInvoiced(t *testing.T) {
	d := newTestDB(t)
	p := mustProject(t, d)
	e, err := d.StartEntry(p.ID, "task")
	if err != nil {
		t.Fatal(err)
	}

	if err := d.SetEntryInvoiced(e.ID, true); err != nil {
		t.Fatalf("SetEntryInvoiced(true): %v", err)
	}
	entries, _ := d.ListEntries(0, "", "", true)
	if !entries[0].Invoiced {
		t.Error("entry should be invoiced")
	}

	if err := d.SetEntryInvoiced(e.ID, false); err != nil {
		t.Fatalf("SetEntryInvoiced(false): %v", err)
	}
	entries, _ = d.ListEntries(0, "", "", true)
	if entries[0].Invoiced {
		t.Error("entry should no longer be invoiced")
	}
}

func TestSetEntryPaid_RequiresInvoiced(t *testing.T) {
	d := newTestDB(t)
	p := mustProject(t, d)
	e, err := d.StartEntry(p.ID, "task")
	if err != nil {
		t.Fatal(err)
	}

	// Not yet invoiced: SetEntryPaid must be a no-op per the WHERE invoiced=1 guard.
	if err := d.SetEntryPaid(e.ID, true); err != nil {
		t.Fatalf("SetEntryPaid: %v", err)
	}
	entries, _ := d.ListEntries(0, "", "", true)
	if entries[0].Paid {
		t.Error("entry should not be payable before it is invoiced")
	}

	if err := d.SetEntryInvoiced(e.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := d.SetEntryPaid(e.ID, true); err != nil {
		t.Fatalf("SetEntryPaid after invoicing: %v", err)
	}
	entries, _ = d.ListEntries(0, "", "", true)
	if !entries[0].Paid {
		t.Error("entry should be paid once invoiced and marked paid")
	}
}

// ── Reports ──────────────────────────────────────────────────────────────────

func TestReportByProject_Aggregation(t *testing.T) {
	d := newTestDB(t)
	c := mustClient(t, d, "Acme") // hourly rate 100
	p, err := d.CreateProject(c.ID, "Website")
	if err != nil {
		t.Fatal(err)
	}

	// Two finished 1-hour entries in February, one invoiced+paid, one invoiced only.
	e1, _ := d.StartEntry(p.ID, "a")
	mustFinishEntry(t, d, e1, "2026-02-01 09:00:00", "2026-02-01 10:00:00")
	if err := d.SetEntryInvoiced(e1.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := d.SetEntryPaid(e1.ID, true); err != nil {
		t.Fatal(err)
	}

	e2, _ := d.StartEntry(p.ID, "b")
	mustFinishEntry(t, d, e2, "2026-02-02 09:00:00", "2026-02-02 10:00:00")
	if err := d.SetEntryInvoiced(e2.ID, true); err != nil {
		t.Fatal(err)
	}

	// A running entry (no end_time) and an entry outside the date range must be excluded.
	if _, err := d.StartEntry(p.ID, "running"); err != nil {
		t.Fatal(err)
	}
	e4, _ := d.StartEntry(p.ID, "outside range")
	mustFinishEntry(t, d, e4, "2026-03-01 09:00:00", "2026-03-01 10:00:00")

	rows, err := d.ReportByProject("2026-02-01", "2026-02-28")
	if err != nil {
		t.Fatalf("ReportByProject: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	r := rows[0]
	if r.ClientName != "Acme" || r.ProjectName != "Website" {
		t.Errorf("row identity = %s/%s, want Acme/Website", r.ClientName, r.ProjectName)
	}
	// ClientID/ProjectID/HourlyRate must be populated: the reports view uses
	// ProjectID to fetch this row's underlying entries for its drill-down
	// (see ReportsModel.loadCmd), so a row with these left at their zero
	// value would silently fetch entries for project/client 0 instead.
	if r.ClientID != c.ID {
		t.Errorf("ClientID = %d, want %d", r.ClientID, c.ID)
	}
	if r.ProjectID != p.ID {
		t.Errorf("ProjectID = %d, want %d", r.ProjectID, p.ID)
	}
	if r.HourlyRate != 100 {
		t.Errorf("HourlyRate = %v, want 100", r.HourlyRate)
	}
	if r.EntryCount != 2 {
		t.Errorf("EntryCount = %d, want 2", r.EntryCount)
	}
	if diff := r.TotalHours - 2; diff < -0.001 || diff > 0.001 {
		t.Errorf("TotalHours = %v, want ~2", r.TotalHours)
	}
	if r.Invoiced != 2 {
		t.Errorf("Invoiced = %d, want 2", r.Invoiced)
	}
	if r.Paid != 1 {
		t.Errorf("Paid = %d, want 1", r.Paid)
	}
	if diff := r.Earnings - 200; diff < -0.1 || diff > 0.1 {
		t.Errorf("Earnings = %v, want ~200 (2 hours * $100/hr)", r.Earnings)
	}
}

func mustFinishEntry(t *testing.T, d *db.DB, e *model.Entry, start, end string) {
	t.Helper()
	st, err := time.Parse("2006-01-02 15:04:05", start)
	if err != nil {
		t.Fatal(err)
	}
	et, err := time.Parse("2006-01-02 15:04:05", end)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.UpdateEntry(&model.Entry{
		ID: e.ID, ProjectID: e.ProjectID, Task: e.Task,
		StartTime: st, EndTime: &et,
	}); err != nil {
		t.Fatalf("mustFinishEntry: %v", err)
	}
}

func TestReportByProject_ExcludesRunningEntries(t *testing.T) {
	d := newTestDB(t)
	p := mustProject(t, d)
	if _, err := d.StartEntry(p.ID, "still running"); err != nil {
		t.Fatal(err)
	}

	rows, err := d.ReportByProject("", "")
	if err != nil {
		t.Fatalf("ReportByProject: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("rows = %+v, want none since the only entry is still running", rows)
	}
}

func TestOpen_CreatesSchemaIdempotently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reopen.db")
	d1, err := db.Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if _, err := d1.CreateClient("Acme", 0); err != nil {
		t.Fatal(err)
	}
	d1.Close()

	d2, err := db.Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer d2.Close()
	clients, err := d2.ListClients()
	if err != nil {
		t.Fatal(err)
	}
	if len(clients) != 1 || clients[0].Name != "Acme" {
		t.Errorf("clients after reopen = %+v, want the previously created Acme", clients)
	}
}

// sanity check that our error-based assertions above would actually catch a
// missing-row condition rather than silently passing on a nil-safe method.
func TestGetProject_NotFoundError(t *testing.T) {
	d := newTestDB(t)
	_, err := d.GetProject(9999)
	if err == nil {
		t.Fatal("GetProject(missing id): want error, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("GetProject error = %q, want it to mention 'not found'", err.Error())
	}
}
