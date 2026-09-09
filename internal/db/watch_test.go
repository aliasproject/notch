package db_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aliasproject/notch/internal/db"
)

func TestWatch_ReturnsNilWhenDirectoryDoesNotExist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	// Open already created the file and its directory; remove the
	// directory out from under the still-open connection to exercise the
	// path Watch is meant to fail gracefully on -- e.g. a NOTCH_DB pointed
	// somewhere never created.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if ch := db.Watch(d); ch != nil {
		t.Error("Watch() should return nil when the DB's directory doesn't exist")
	}
}

// TestWatch_FiresOnRealWrite is the important one: does an actual external
// write -- through a *separate* db.Open/Close, not the connection being
// watched -- fire the watch while the watched connection stays open the
// whole time. That's the shape Watch exists for: a long-running consumer
// (the TUI, holding its own open connection throughout) watching for
// writes from short-lived CLI processes (another notch invocation, or the
// plugin) that open, write, and exit. An earlier version of Watch fired
// only after SQLite's WAL checkpoint landed in the main file, which
// requires the *last* connection to close -- never true here since the
// watched connection never closes -- so this specifically guards against
// that regression.
func TestWatch_FiresOnRealWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	ch := db.Watch(d)
	if ch == nil {
		t.Fatal("Watch() = nil, want a channel (the DB's directory exists)")
	}

	select {
	case <-ch:
		t.Fatal("Watch() fired before any write")
	case <-time.After(200 * time.Millisecond):
	}

	func() {
		writer, err := db.Open(path)
		if err != nil {
			t.Fatalf("db.Open (writer): %v", err)
		}
		defer writer.Close()

		c, err := writer.CreateClient("Acme", 0)
		if err != nil {
			t.Fatalf("CreateClient: %v", err)
		}
		p, err := writer.CreateProject(c.ID, "Website")
		if err != nil {
			t.Fatalf("CreateProject: %v", err)
		}
		if _, err := writer.StartEntry(p.ID, "test task"); err != nil {
			t.Fatalf("StartEntry: %v", err)
		}
	}()

	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("Watch() never fired after a separate process wrote to the DB")
	}
}

// TestWatch_DoesNotFireOnReads guards against the exact bug Watch used to
// have: watching "<path>-wal" (or the directory containing it, treating
// any event on it as a change) fires on every notch invocation, reads
// included, since WAL-mode reads still open/checkpoint/close that file.
// A consumer that reloads on every fire, and whose reload itself reads the
// DB, would then be self-triggering -- effectively a busy poll. Watch
// guards against this with a PRAGMA data_version check gating the actual
// fire; this test exercises that gate directly, using separate short-lived
// db.Open/Close pairs per read to mirror how a real "notch status -json"/
// "notch tasks -json" invocation touches the DB.
func TestWatch_DoesNotFireOnReads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	c, err := d.CreateClient("Acme", 0)
	if err != nil {
		t.Fatalf("CreateClient: %v", err)
	}
	if _, err := d.CreateProject(c.ID, "Website"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	ch := db.Watch(d)
	if ch == nil {
		t.Fatal("Watch() = nil, want a channel (the DB's directory exists)")
	}

	for i := 0; i < 5; i++ {
		func() {
			reader, err := db.Open(path)
			if err != nil {
				t.Fatalf("db.Open (reader): %v", err)
			}
			defer reader.Close()

			if _, err := reader.GetRunningEntry(); err != nil {
				t.Fatalf("GetRunningEntry: %v", err)
			}
			if _, err := reader.ListProjects(0); err != nil {
				t.Fatalf("ListProjects: %v", err)
			}
		}()
	}

	select {
	case <-ch:
		t.Fatal("Watch() fired after reads only -- no write happened")
	case <-time.After(500 * time.Millisecond):
	}
}
