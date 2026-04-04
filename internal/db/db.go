package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/maguiard/timetui/internal/model"
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
	sql *sql.DB
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
	return &DB{sql: sqldb}, nil
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
         ORDER BY e.start_time DESC LIMIT 1`)
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
	q += ` ORDER BY e.start_time DESC`
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
	q := `SELECT c.id, c.name, c.hourly_rate,
                 p.id, p.name,
                 SUM((JULIANDAY(COALESCE(e.end_time, CURRENT_TIMESTAMP)) - JULIANDAY(e.start_time)) * 24) AS hours,
                 COUNT(*) AS entry_count,
                 SUM(CASE WHEN e.invoiced=1 THEN 1 ELSE 0 END) AS invoiced,
                 SUM(CASE WHEN e.paid=1 THEN 1 ELSE 0 END) AS paid
          FROM entries e
          JOIN projects p ON p.id = e.project_id
          JOIN clients  c ON c.id = p.client_id
          WHERE 1=1`
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
		if err := rows.Scan(
			&r.ClientID, &r.ClientName, &r.HourlyRate,
			&r.ProjectID, &r.ProjectName,
			&r.TotalHours, &r.EntryCount, &r.Invoiced, &r.Paid,
		); err != nil {
			return nil, err
		}
		r.Earnings = r.TotalHours * r.HourlyRate
		report = append(report, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Attach individual entries to each report row
	allEntries, err := d.ListEntries(0, dateFrom, dateTo, false)
	if err != nil {
		return nil, err
	}
	// Index entries by project ID
	byProject := make(map[int64][]*model.Entry, len(allEntries))
	for _, e := range allEntries {
		byProject[e.ProjectID] = append(byProject[e.ProjectID], e)
	}
	for _, r := range report {
		r.Entries = byProject[r.ProjectID]
	}

	return report, nil
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
