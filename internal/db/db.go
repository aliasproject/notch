package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/aliasproject/notch/internal/model"
	_ "modernc.org/sqlite"
)

// sqliteTimeFormats lists every datetime format SQLite / modernc may return.
// We always parse in UTC since we store all times as UTC strings.
var sqliteTimeFormats = []string{
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"2006-01-02T15:04:05Z",
	"2006-01-02 15:04:05Z",
	"2006-01-02T15:04:05-07:00",
	"2006-01-02 15:04:05-07:00",
	"2006-01-02",
}

// parseTime tries each known SQLite datetime format and always returns a UTC time.
func parseTime(s string) time.Time {
	for _, layout := range sqliteTimeFormats {
		if t, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			return t
		}
	}
	// fallback: return zero time — callers will see Jan 1 0001 which is obvious
	return time.Time{}
}

const schema = `
CREATE TABLE IF NOT EXISTS clients (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT NOT NULL UNIQUE,
    hourly_rate  REAL NOT NULL DEFAULT 0,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS projects (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    client_id  INTEGER NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(client_id, name)
);

CREATE TABLE IF NOT EXISTS entries (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id  INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    task        TEXT NOT NULL,
    start_time  DATETIME NOT NULL,
    end_time    DATETIME,
    invoiced    INTEGER NOT NULL DEFAULT 0,
    paid        INTEGER NOT NULL DEFAULT 0,
    notes       TEXT NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

type DB struct {
	sql  *sql.DB
	path string
}

// Open opens (or creates) the SQLite database at the given path.
func Open(path string) (*DB, error) {
	sqldb, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	sqldb.SetMaxOpenConns(1) // sqlite is single-writer
	if _, err := sqldb.Exec("PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;"); err != nil {
		return nil, fmt.Errorf("pragma: %w", err)
	}
	if _, err := sqldb.Exec(schema); err != nil {
		return nil, fmt.Errorf("schema: %w", err)
	}
	return &DB{sql: sqldb, path: path}, nil
}

// Path returns the filesystem path this DB was opened from -- e.g. for
// Watch, which needs it but isn't itself a DB method (it has to work
// before/without an open connection, and doesn't want one).
func (d *DB) Path() string {
	return d.path
}

func (d *DB) Close() error { return d.sql.Close() }

// -- Clients ------------------------------------------------------------------

func (d *DB) CreateClient(name string, hourlyRate float64) (*model.Client, error) {
	res, err := d.sql.Exec(
		`INSERT INTO clients (name, hourly_rate) VALUES (?, ?)`, name, hourlyRate)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &model.Client{ID: id, Name: name, HourlyRate: hourlyRate, CreatedAt: time.Now()}, nil
}

func (d *DB) UpdateClient(c *model.Client) error {
	_, err := d.sql.Exec(
		`UPDATE clients SET name=?, hourly_rate=? WHERE id=?`, c.Name, c.HourlyRate, c.ID)
	return err
}

func (d *DB) DeleteClient(id int64) error {
	_, err := d.sql.Exec(`DELETE FROM clients WHERE id=?`, id)
	return err
}

func (d *DB) ListClients() ([]*model.Client, error) {
	rows, err := d.sql.Query(
		`SELECT id, name, hourly_rate, created_at FROM clients ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var clients []*model.Client
	for rows.Next() {
		c := &model.Client{}
		var createdAt string
		if err := rows.Scan(&c.ID, &c.Name, &c.HourlyRate, &createdAt); err != nil {
			return nil, err
		}
		c.CreatedAt = parseTime(createdAt)
		clients = append(clients, c)
	}
	return clients, rows.Err()
}

func (d *DB) GetClient(id int64) (*model.Client, error) {
	c := &model.Client{}
	var createdAt string
	err := d.sql.QueryRow(
		`SELECT id, name, hourly_rate, created_at FROM clients WHERE id=?`, id).
		Scan(&c.ID, &c.Name, &c.HourlyRate, &createdAt)
	if err != nil {
		return nil, err
	}
	c.CreatedAt = parseTime(createdAt)
	return c, nil
}

// -- Projects -----------------------------------------------------------------

func (d *DB) CreateProject(clientID int64, name string) (*model.Project, error) {
	res, err := d.sql.Exec(
		`INSERT INTO projects (client_id, name) VALUES (?, ?)`, clientID, name)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &model.Project{ID: id, ClientID: clientID, Name: name, CreatedAt: time.Now()}, nil
}

func (d *DB) UpdateProject(p *model.Project) error {
	_, err := d.sql.Exec(
		`UPDATE projects SET client_id=?, name=? WHERE id=?`, p.ClientID, p.Name, p.ID)
	return err
}

func (d *DB) DeleteProject(id int64) error {
	_, err := d.sql.Exec(`DELETE FROM projects WHERE id=?`, id)
	return err
}

func (d *DB) ListProjects(clientID int64) ([]*model.Project, error) {
	q := `SELECT p.id, p.client_id, p.name, p.created_at,
                 c.id, c.name, c.hourly_rate, c.created_at
          FROM projects p JOIN clients c ON c.id = p.client_id`
	var rows *sql.Rows
	var err error
	if clientID > 0 {
		rows, err = d.sql.Query(q+` WHERE p.client_id=? ORDER BY p.name`, clientID)
	} else {
		rows, err = d.sql.Query(q + ` ORDER BY c.name, p.name`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanProjects(rows)
}

func (d *DB) GetProject(id int64) (*model.Project, error) {
	rows, err := d.sql.Query(
		`SELECT p.id, p.client_id, p.name, p.created_at,
                c.id, c.name, c.hourly_rate, c.created_at
         FROM projects p JOIN clients c ON c.id = p.client_id
         WHERE p.id=?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ps, err := scanProjects(rows)
	if err != nil || len(ps) == 0 {
		return nil, fmt.Errorf("project not found: %d", id)
	}
	return ps[0], nil
}

func scanProjects(rows *sql.Rows) ([]*model.Project, error) {
	var projects []*model.Project
	for rows.Next() {
		p := &model.Project{Client: &model.Client{}}
		var pCreated, cCreated string
		err := rows.Scan(
			&p.ID, &p.ClientID, &p.Name, &pCreated,
			&p.Client.ID, &p.Client.Name, &p.Client.HourlyRate, &cCreated)
		if err != nil {
			return nil, err
		}
		p.CreatedAt = parseTime(pCreated)
		p.Client.CreatedAt = parseTime(cCreated)
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

// -- Entries ------------------------------------------------------------------

func (d *DB) StartEntry(projectID int64, task string) (*model.Entry, error) {
	now := time.Now().UTC()
	res, err := d.sql.Exec(
		`INSERT INTO entries (project_id, task, start_time) VALUES (?, ?, ?)`,
		projectID, task, now.Format("2006-01-02 15:04:05"))
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &model.Entry{
		ID: id, ProjectID: projectID, Task: task, StartTime: now,
	}, nil
}

func (d *DB) StopEntry(id int64) error {
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	_, err := d.sql.Exec(`UPDATE entries SET end_time=? WHERE id=? AND end_time IS NULL`, now, id)
	return err
}

func (d *DB) StopAllRunning() error {
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	_, err := d.sql.Exec(`UPDATE entries SET end_time=? WHERE end_time IS NULL`, now)
	return err
}

func (d *DB) UpdateEntry(e *model.Entry) error {
	var endTime interface{}
	if e.EndTime != nil {
		endTime = e.EndTime.UTC().Format("2006-01-02 15:04:05")
	}
	_, err := d.sql.Exec(
		`UPDATE entries SET project_id=?, task=?, start_time=?, end_time=?,
         invoiced=?, paid=?, notes=? WHERE id=?`,
		e.ProjectID,
		e.Task,
		e.StartTime.UTC().Format("2006-01-02 15:04:05"),
		endTime,
		boolToInt(e.Invoiced),
		boolToInt(e.Paid),
		e.Notes,
		e.ID,
	)
	return err
}

func (d *DB) DeleteEntry(id int64) error {
	_, err := d.sql.Exec(`DELETE FROM entries WHERE id=?`, id)
	return err
}

func (d *DB) GetRunningEntry() (*model.Entry, error) {
	rows, err := d.sql.Query(
		`SELECT e.id, e.project_id, e.task, e.start_time, e.end_time,
                e.invoiced, e.paid, e.notes, e.created_at,
                p.id, p.client_id, p.name, p.created_at,
                c.id, c.name, c.hourly_rate, c.created_at
         FROM entries e
         JOIN projects p ON p.id = e.project_id
         JOIN clients  c ON c.id = p.client_id
         WHERE e.end_time IS NULL
         ORDER BY e.start_time DESC, e.id DESC LIMIT 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries, err := scanEntries(rows)
	if err != nil || len(entries) == 0 {
		return nil, nil
	}
	return entries[0], nil
}

// ListEntries lists entries with optional filters. Pass zero values to skip a filter.
// dateFrom/dateTo format: "2006-01-02"
func (d *DB) ListEntries(projectID int64, dateFrom, dateTo string, includeRunning bool) ([]*model.Entry, error) {
	q := `SELECT e.id, e.project_id, e.task, e.start_time, e.end_time,
                 e.invoiced, e.paid, e.notes, e.created_at,
                 p.id, p.client_id, p.name, p.created_at,
                 c.id, c.name, c.hourly_rate, c.created_at
          FROM entries e
          JOIN projects p ON p.id = e.project_id
          JOIN clients  c ON c.id = p.client_id
          WHERE 1=1`
	args := []interface{}{}
	if projectID > 0 {
		q += ` AND e.project_id=?`
		args = append(args, projectID)
	}
	if dateFrom != "" {
		q += ` AND date(e.start_time)>=?`
		args = append(args, dateFrom)
	}
	if dateTo != "" {
		q += ` AND date(e.start_time)<=?`
		args = append(args, dateTo)
	}
	if !includeRunning {
		q += ` AND e.end_time IS NOT NULL`
	}
	q += ` ORDER BY e.start_time DESC, e.id DESC`
	rows, err := d.sql.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEntries(rows)
}

// SetEntryInvoiced marks one or more entries as invoiced/uninvoiced.
func (d *DB) SetEntryInvoiced(id int64, invoiced bool) error {
	_, err := d.sql.Exec(`UPDATE entries SET invoiced=? WHERE id=?`, boolToInt(invoiced), id)
	return err
}

// SetEntryPaid marks one or more entries as paid/unpaid.
func (d *DB) SetEntryPaid(id int64, paid bool) error {
	_, err := d.sql.Exec(`UPDATE entries SET paid=? WHERE id=? AND invoiced=1`, boolToInt(paid), id)
	return err
}

// -- Reports ------------------------------------------------------------------

// ReportByProject returns aggregated hours/earnings grouped by client + project,
// with individual entries attached to each row for drill-down display.
func (d *DB) ReportByProject(dateFrom, dateTo string) ([]*model.ReportRow, error) {
	q := `SELECT c.id, p.id, c.name, p.name,
                 SUM((JULIANDAY(COALESCE(e.end_time, CURRENT_TIMESTAMP)) - JULIANDAY(e.start_time)) * 24) AS hours,
                 COUNT(*) AS entry_count,
                 SUM(CASE WHEN e.invoiced=1 THEN 1 ELSE 0 END) AS invoiced,
                 SUM(CASE WHEN e.paid=1 THEN 1 ELSE 0 END) AS paid,
                 c.hourly_rate
          FROM entries e
          JOIN projects p ON p.id = e.project_id
          JOIN clients  c ON c.id = p.client_id
          WHERE e.end_time IS NOT NULL`
	args := []interface{}{}
	if dateFrom != "" {
		q += ` AND date(e.start_time)>=?`
		args = append(args, dateFrom)
	}
	if dateTo != "" {
		q += ` AND date(e.start_time)<=?`
		args = append(args, dateTo)
	}
	q += ` GROUP BY c.id, p.id ORDER BY c.name, p.name`

	rows, err := d.sql.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var report []*model.ReportRow
	for rows.Next() {
		r := &model.ReportRow{}
		if err := rows.Scan(&r.ClientID, &r.ProjectID, &r.ClientName, &r.ProjectName, &r.TotalHours,
			&r.EntryCount, &r.Invoiced, &r.Paid, &r.HourlyRate); err != nil {
			return nil, err
		}
		r.Earnings = r.TotalHours * r.HourlyRate
		report = append(report, r)
	}
	return report, rows.Err()
}

// ListRecentTasks returns the most recently-used distinct (project, task)
// pairings, most recent first, each with its accumulated duration across
// every finished entry that used that exact task string on that project --
// for a picker that wants to resume a specific named task (e.g. "Meeting"
// on "Website") rather than just start a bare project. Like
// ReportByProject, running entries are excluded from the total; a task
// that's never been finished before won't appear here until it has.
func (d *DB) ListRecentTasks(limit int) ([]*model.RecentTask, error) {
	rows, err := d.sql.Query(
		`SELECT e.project_id, p.name, c.name, e.task,
                SUM((JULIANDAY(e.end_time) - JULIANDAY(e.start_time)) * 86400) AS total_seconds,
                MAX(e.start_time) AS last_used
         FROM entries e
         JOIN projects p ON p.id = e.project_id
         JOIN clients  c ON c.id = p.client_id
         WHERE e.end_time IS NOT NULL
         GROUP BY e.project_id, e.task
         ORDER BY last_used DESC
         LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []*model.RecentTask
	for rows.Next() {
		t := &model.RecentTask{}
		var totalSeconds float64
		var lastUsed string
		if err := rows.Scan(&t.ProjectID, &t.ProjectName, &t.ClientName, &t.Task, &totalSeconds, &lastUsed); err != nil {
			return nil, err
		}
		t.TotalSeconds = int64(totalSeconds)
		t.LastUsed = parseTime(lastUsed)
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// ResolveOrCreateProject finds or creates the client+project and returns the
// project ID. Returns 0 if both clientName and projectName are blank
// (uncategorized). Moved here (from what was internal/ui/views'
// unexported resolveProject) so the CLI ("notch start --client-name/
// --project-name") and the TUI's own new-timer form share one
// implementation instead of the CLI needing its own copy or an import of
// the ui/views package.
func (d *DB) ResolveOrCreateProject(clientID int64, clientName string, projectID int64, projectName string) (int64, error) {
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
		clients, err := d.ListClients()
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
			c, err := d.CreateClient(clientName, 0)
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
		projects, err := d.ListProjects(clientID)
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
			c, err := d.CreateClient(projectName, 0)
			if err != nil {
				return 0, fmt.Errorf("create client %q: %w", projectName, err)
			}
			clientID = c.ID
		}
		p, err := d.CreateProject(clientID, projectName)
		if err != nil {
			return 0, fmt.Errorf("create project %q: %w", projectName, err)
		}
		return p.ID, nil
	}

	return 0, nil
}

// EnsureUncategorizedProject returns (or creates) a catch-all "Uncategorized
// / General" project for untagged timers -- e.g. a quick-add that only
// specifies a task, no client/project. Moved here (from what was
// internal/ui/views' unexported ensureUncategorizedProject) alongside
// ResolveOrCreateProject, for the same reason: the CLI needs it too, not
// just the TUI's own new-timer form.
func (d *DB) EnsureUncategorizedProject() (int64, error) {
	const clientName = "Uncategorized"
	const projectName = "General"

	clients, err := d.ListClients()
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
		c, err := d.CreateClient(clientName, 0)
		if err != nil {
			return 0, err
		}
		clientID = c.ID
	}

	projects, err := d.ListProjects(clientID)
	if err != nil {
		return 0, err
	}
	for _, p := range projects {
		if p.Name == projectName {
			return p.ID, nil
		}
	}
	p, err := d.CreateProject(clientID, projectName)
	if err != nil {
		return 0, err
	}
	return p.ID, nil
}

// -- helpers ------------------------------------------------------------------

func scanEntries(rows *sql.Rows) ([]*model.Entry, error) {
	var entries []*model.Entry
	for rows.Next() {
		e := &model.Entry{Project: &model.Project{Client: &model.Client{}}}
		var startTime, eCreated, pCreated, cCreated string
		var endTimeNull sql.NullString
		err := rows.Scan(
			&e.ID, &e.ProjectID, &e.Task, &startTime, &endTimeNull,
			&e.Invoiced, &e.Paid, &e.Notes, &eCreated,
			&e.Project.ID, &e.Project.ClientID, &e.Project.Name, &pCreated,
			&e.Project.Client.ID, &e.Project.Client.Name,
			&e.Project.Client.HourlyRate, &cCreated,
		)
		if err != nil {
			return nil, err
		}
		e.StartTime = parseTime(startTime)
		if endTimeNull.Valid {
			t := parseTime(endTimeNull.String)
			e.EndTime = &t
		}
		e.CreatedAt = parseTime(eCreated)
		e.Project.CreatedAt = parseTime(pCreated)
		e.Project.Client.CreatedAt = parseTime(cCreated)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
