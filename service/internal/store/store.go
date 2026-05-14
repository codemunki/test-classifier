package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/mdoherty/test-classifier/internal/classifier"
	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS runs (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	test_id      TEXT    NOT NULL,
	run_id       TEXT    NOT NULL,
	status       TEXT    NOT NULL,
	duration_ms  INTEGER NOT NULL DEFAULT 0,
	started_at   TEXT    NOT NULL,
	error_message TEXT   NOT NULL DEFAULT '',
	UNIQUE(run_id)
);
CREATE INDEX IF NOT EXISTS idx_runs_test_started ON runs(test_id, started_at);
`

// Store persists run records and retrieves history by test ID.
// A single-connection write pool serialises writes; a separate read pool
// allows concurrent reads under SQLite's WAL mode.
type Store struct {
	write *sql.DB
	read  *sql.DB
}

// Open opens (or creates) a SQLite database at the given path.
// Use ":memory:" for an in-process test database.
func Open(path string) (*Store, error) {
	write, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open write db: %w", err)
	}
	// Single connection serialises all writes; WAL ensures readers are not blocked.
	write.SetMaxOpenConns(1)
	write.SetMaxIdleConns(1)
	if _, err := write.Exec(`
		PRAGMA journal_mode=WAL;
		PRAGMA busy_timeout=5000;
		PRAGMA synchronous=NORMAL;
	`); err != nil {
		write.Close()
		return nil, fmt.Errorf("configure write db: %w", err)
	}
	if _, err := write.Exec(schema); err != nil {
		write.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	// In-memory databases are connection-scoped: a second connection would see
	// an empty database. Share the write connection for reads in that case.
	if path == ":memory:" {
		return &Store{write: write, read: write}, nil
	}

	read, err := sql.Open("sqlite", path)
	if err != nil {
		write.Close()
		return nil, fmt.Errorf("open read db: %w", err)
	}
	// WAL allows multiple concurrent readers alongside the single writer.
	read.SetMaxOpenConns(10)
	read.SetMaxIdleConns(10)
	if _, err := read.Exec(`PRAGMA busy_timeout=5000;`); err != nil {
		write.Close()
		read.Close()
		return nil, fmt.Errorf("configure read db: %w", err)
	}

	return &Store{write: write, read: read}, nil
}

// Close closes the underlying database connections.
func (s *Store) Close() error {
	werr := s.write.Close()
	if s.read == s.write {
		return werr
	}
	rerr := s.read.Close()
	if werr != nil {
		return werr
	}
	return rerr
}

// InsertRun persists a single run record. Duplicate run_ids are silently ignored.
func (s *Store) InsertRun(r classifier.Run) error {
	_, err := s.write.Exec(
		`INSERT OR IGNORE INTO runs (test_id, run_id, status, duration_ms, started_at, error_message)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		r.TestID, r.RunID, r.Status, r.DurationMS,
		r.StartedAt.UTC().Format(time.RFC3339),
		r.ErrorMessage,
	)
	if err != nil {
		return fmt.Errorf("insert run: %w", err)
	}
	return nil
}

// GetHistory returns up to limit of the most recent runs for testID, ordered oldest first.
func (s *Store) GetHistory(testID string, limit int) ([]classifier.Run, error) {
	rows, err := s.read.Query(
		`SELECT test_id, run_id, status, duration_ms, started_at, error_message
		 FROM (
		   SELECT test_id, run_id, status, duration_ms, started_at, error_message
		   FROM runs
		   WHERE test_id = ?
		   ORDER BY started_at DESC
		   LIMIT ?
		 )
		 ORDER BY started_at ASC`,
		testID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query history: %w", err)
	}
	defer rows.Close()

	var runs []classifier.Run
	for rows.Next() {
		var r classifier.Run
		var startedAt string
		if err := rows.Scan(&r.TestID, &r.RunID, &r.Status, &r.DurationMS, &startedAt, &r.ErrorMessage); err != nil {
			return nil, fmt.Errorf("scan run: %w", err)
		}
		r.StartedAt, err = time.Parse(time.RFC3339, startedAt)
		if err != nil {
			return nil, fmt.Errorf("parse started_at: %w", err)
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// TestIDs returns all distinct test IDs in the store.
func (s *Store) TestIDs() ([]string, error) {
	rows, err := s.read.Query(`SELECT DISTINCT test_id FROM runs ORDER BY test_id`)
	if err != nil {
		return nil, fmt.Errorf("query test ids: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan test id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
