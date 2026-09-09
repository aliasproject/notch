package db

import (
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watch starts watching the filesystem for external changes to the
// database -- a write from another process (another "notch" CLI
// invocation, or the aliasOS/Omarchy bar-widget plugin, both of which are
// just short-lived CLI calls with no way to otherwise notify a long-running
// consumer) -- and returns a channel that receives a value shortly after,
// a fast trigger for a long-running consumer (the TUI) to reload rather
// than wait for its next poll tick. Mirrors internal/theme's own Watch():
// same reasoning, same debounce, same "nil channel on failure, caller
// falls back to polling" contract.
//
// Getting this right took two wrong turns, both confirmed with inotifywait
// against a real WAL-mode DB:
//
//  1. Watching "<path>-wal" (or the directory containing it, catching -wal
//     along with everything else): every notch invocation, reads included,
//     opens/checkpoints/closes that file, deleting and recreating it each
//     time. A watch on it fires on *reads* too, and since d's own reload
//     after a fire itself reads the DB, that's a self-triggering loop -- a
//     "poll as fast as possible" watch, not the efficient one this is
//     meant to be.
//  2. Watching "<path>" itself instead: a genuine write only produces a
//     MODIFY event on it once SQLite checkpoints the WAL back in, and
//     SQLite only auto-checkpoints on the *last* connection to the file
//     closing -- which never happens while d itself (the long-running
//     consumer's own connection, e.g. the TUI's) stays open. So this
//     fixed the self-triggering problem but then almost never fired
//     during the one scenario Watch exists for: the consumer running
//     with an open connection, watching for someone else's write.
//
// The actual fix doesn't try to infer "did the DB change" from *which*
// file got touched or how -- it asks SQLite directly, via `PRAGMA
// data_version` on d's own connection. That pragma is specifically
// designed for this: it changes the instant any *other* connection commits
// a write (no checkpoint required -- confirmed by querying it on an
// already-open connection immediately after a separate connection's
// write), and it does not change on reads, from this connection or any
// other. So the directory watch below still exists (still watching
// "<path>", "-wal" and "-shm" together, whichever a given write happens to
// touch) purely as a low-cost "something might have happened, go check"
// trigger; the data_version comparison is what actually decides whether
// to notify the caller, making a spurious wake from a pure read a no-op
// instead of a self-triggering reload.
//
// Returns nil if the OS watcher couldn't be started, the directory doesn't
// exist (e.g. a NOTCH_DB pointed somewhere never created), or the initial
// data_version read fails; callers should treat a nil channel as one that
// never fires and keep their own poll fallback.
func Watch(d *DB) <-chan struct{} {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil
	}

	if err := w.Add(filepath.Dir(d.path)); err != nil {
		w.Close()
		return nil
	}

	lastVersion, err := d.dataVersion()
	if err != nil {
		w.Close()
		return nil
	}

	out := make(chan struct{}, 1)
	go func() {
		defer w.Close()
		defer close(out)

		var debounce *time.Timer
		var debounceC <-chan time.Time
		for {
			select {
			case _, ok := <-w.Events:
				if !ok {
					return
				}
				// Coalesce the burst of events one write produces (the
				// main file, -wal, and -shm can each fire their own
				// event) into one data_version check, fired 75ms after
				// the last event rather than the first -- same interval
				// theme.Watch() uses for the same reason.
				if debounce == nil {
					debounce = time.NewTimer(75 * time.Millisecond)
				} else {
					if !debounce.Stop() {
						select {
						case <-debounce.C:
						default:
						}
					}
					debounce.Reset(75 * time.Millisecond)
				}
				debounceC = debounce.C

			case <-debounceC:
				debounceC = nil
				v, err := d.dataVersion()
				if err != nil || v == lastVersion {
					continue
				}
				lastVersion = v
				select {
				case out <- struct{}{}:
				default:
				}

			case _, ok := <-w.Errors:
				if !ok {
					return
				}
			}
		}
	}()
	return out
}

// dataVersion reads SQLite's data_version counter, which increments the
// instant any *other* connection commits a write to this database and
// never changes on a read -- see Watch's own doc comment for why that
// makes it the right signal for "did an external process change this DB".
func (d *DB) dataVersion() (int64, error) {
	var v int64
	err := d.sql.QueryRow("PRAGMA data_version").Scan(&v)
	return v, err
}
