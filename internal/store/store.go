// Package store persists requests and the tamper-evident audit log in SQLite.
package store

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

// Store wraps the database. Writes are serialized so the audit hash chain
// stays linear.
type Store struct {
	db *sql.DB
	mu sync.Mutex
}

const schema = `
CREATE TABLE IF NOT EXISTS requests (
  id           TEXT PRIMARY KEY,
  created_at   INTEGER NOT NULL,
  requester    TEXT NOT NULL,
  requester_kind TEXT NOT NULL,
  target       TEXT NOT NULL,
  argv         TEXT NOT NULL,       -- JSON array
  reason       TEXT NOT NULL DEFAULT '',
  level        INTEGER NOT NULL,
  rule         TEXT NOT NULL,
  status       TEXT NOT NULL,       -- pending/approved/rejected/expired/running/succeeded/failed/denied
  approver     TEXT NOT NULL DEFAULT '',
  decided_at   INTEGER NOT NULL DEFAULT 0,
  decision_note TEXT NOT NULL DEFAULT '',
  expires_at   INTEGER NOT NULL DEFAULT 0,
  started_at   INTEGER NOT NULL DEFAULT 0,
  finished_at  INTEGER NOT NULL DEFAULT 0,
  exit_code    INTEGER NOT NULL DEFAULT 0,
  stdout       TEXT NOT NULL DEFAULT '',
  stderr       TEXT NOT NULL DEFAULT '',
  truncated    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS requests_status ON requests(status, created_at);

CREATE TABLE IF NOT EXISTS audit (
  seq        INTEGER PRIMARY KEY AUTOINCREMENT,
  ts         INTEGER NOT NULL,
  actor      TEXT NOT NULL,
  actor_kind TEXT NOT NULL,
  action     TEXT NOT NULL,
  request_id TEXT NOT NULL DEFAULT '',
  target     TEXT NOT NULL DEFAULT '',
  detail     TEXT NOT NULL DEFAULT '',  -- JSON
  remote     TEXT NOT NULL DEFAULT '',
  prev_hash  TEXT NOT NULL,
  hash       TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS audit_request ON audit(request_id);
CREATE INDEX IF NOT EXISTS audit_actor ON audit(actor, ts);

CREATE TABLE IF NOT EXISTS custom_rules (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  name       TEXT NOT NULL UNIQUE,
  program    TEXT NOT NULL,
  args       TEXT NOT NULL DEFAULT '',
  not_args   TEXT NOT NULL DEFAULT '',
  level      INTEGER NOT NULL,
  target     TEXT NOT NULL DEFAULT '',
  priority   INTEGER NOT NULL DEFAULT 100,
  enabled    INTEGER NOT NULL DEFAULT 1,
  note       TEXT NOT NULL DEFAULT '',
  created_by TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_by TEXT NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS custom_commands (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT NOT NULL UNIQUE,
  description TEXT NOT NULL DEFAULT '',
  template    TEXT NOT NULL,
  params      TEXT NOT NULL DEFAULT '[]',  -- JSON
  level       INTEGER NOT NULL,
  targets     TEXT NOT NULL DEFAULT '[]',  -- JSON
  enabled     INTEGER NOT NULL DEFAULT 1,
  created_by  TEXT NOT NULL,
  created_at  INTEGER NOT NULL,
  updated_by  TEXT NOT NULL,
  updated_at  INTEGER NOT NULL
);
`

// migrations are idempotent ALTERs for databases created by older versions.
var migrations = []string{
	`ALTER TABLE requests ADD COLUMN command TEXT NOT NULL DEFAULT ''`,
}

// Open opens (and migrates) the database.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	for _, m := range migrations {
		if _, err := db.Exec(m); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return nil, fmt.Errorf("migrate: %w", err)
		}
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }
