package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// Request statuses.
const (
	StatusPending   = "pending"
	StatusApproved  = "approved"
	StatusRejected  = "rejected"
	StatusExpired   = "expired"
	StatusCancelled = "cancelled"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusDenied    = "denied"
)

// ErrNotFound is returned when a request does not exist.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned when a state transition is not allowed.
var ErrConflict = errors.New("request is not in the expected state")

// Request is a command submitted for execution.
type Request struct {
	ID            string   `json:"id"`
	CreatedAt     int64    `json:"created_at"`
	Requester     string   `json:"requester"`
	RequesterKind string   `json:"requester_kind"`
	Target        string   `json:"target"`
	Argv          []string `json:"argv"`
	Reason        string   `json:"reason"`
	Level         int      `json:"level"`
	Rule          string   `json:"rule"`
	Status        string   `json:"status"`
	Approver      string   `json:"approver,omitempty"`
	DecidedAt     int64    `json:"decided_at,omitempty"`
	DecisionNote  string   `json:"decision_note,omitempty"`
	ExpiresAt     int64    `json:"expires_at,omitempty"`
	StartedAt     int64    `json:"started_at,omitempty"`
	FinishedAt    int64    `json:"finished_at,omitempty"`
	ExitCode      int      `json:"exit_code"`
	Stdout        string   `json:"stdout,omitempty"`
	Stderr        string   `json:"stderr,omitempty"`
	Truncated     bool     `json:"truncated,omitempty"`
}

// NewID returns a short random request id.
func NewID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "req_" + hex.EncodeToString(b)
}

const reqCols = `id,created_at,requester,requester_kind,target,argv,reason,level,rule,status,approver,decided_at,
decision_note,expires_at,started_at,finished_at,exit_code,stdout,stderr,truncated`

func scanRequest(sc interface{ Scan(...any) error }) (*Request, error) {
	var r Request
	var argv string
	var trunc int
	err := sc.Scan(&r.ID, &r.CreatedAt, &r.Requester, &r.RequesterKind, &r.Target, &argv, &r.Reason, &r.Level, &r.Rule,
		&r.Status, &r.Approver, &r.DecidedAt, &r.DecisionNote, &r.ExpiresAt, &r.StartedAt, &r.FinishedAt,
		&r.ExitCode, &r.Stdout, &r.Stderr, &trunc)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	r.Truncated = trunc != 0
	_ = json.Unmarshal([]byte(argv), &r.Argv)
	return &r, nil
}

// CreateRequest inserts r and writes the matching audit event atomically
// with respect to other writers.
func (s *Store) CreateRequest(r *Request, remote string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	argv, _ := json.Marshal(r.Argv)
	if r.CreatedAt == 0 {
		r.CreatedAt = time.Now().UnixMilli()
	}
	_, err := s.db.Exec(`INSERT INTO requests(id,created_at,requester,requester_kind,target,argv,reason,level,rule,status,expires_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.CreatedAt, r.Requester, r.RequesterKind, r.Target, string(argv), r.Reason, r.Level, r.Rule, r.Status, r.ExpiresAt)
	if err != nil {
		return err
	}
	return s.auditLocked(r.Requester, r.RequesterKind, "request.create", r.ID, r.Target, remote, map[string]any{
		"argv": r.Argv, "reason": r.Reason, "level": r.Level, "rule": r.Rule, "status": r.Status,
	})
}

// GetRequest loads a request.
func (s *Store) GetRequest(id string) (*Request, error) {
	return scanRequest(s.db.QueryRow(`SELECT `+reqCols+` FROM requests WHERE id=?`, id))
}

// RequestFilter narrows ListRequests.
type RequestFilter struct {
	Status    string
	Requester string
	Limit     int
}

// ListRequests returns requests newest first.
func (s *Store) ListRequests(f RequestFilter) ([]*Request, error) {
	q := `SELECT ` + reqCols + ` FROM requests WHERE 1=1`
	var args []any
	if f.Status != "" {
		q += ` AND status=?`
		args = append(args, f.Status)
	}
	if f.Requester != "" {
		q += ` AND requester=?`
		args = append(args, f.Requester)
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, f.Limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Request{}
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Transition moves a request from one of `from` statuses to `to`, applying
// set (column -> value) and recording an audit event in the same critical
// section. It returns ErrConflict if the current status is not in from.
func (s *Store) Transition(id string, from []string, to string, set map[string]any,
	actor, kind, action, remote string, detail any) (*Request, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := scanRequest(s.db.QueryRow(`SELECT `+reqCols+` FROM requests WHERE id=?`, id))
	if err != nil {
		return nil, err
	}
	ok := false
	for _, f := range from {
		if r.Status == f {
			ok = true
		}
	}
	if !ok {
		return r, ErrConflict
	}
	q := `UPDATE requests SET status=?`
	args := []any{to}
	for k, v := range set {
		switch k { // whitelist columns
		case "approver", "decided_at", "decision_note", "started_at", "finished_at", "exit_code", "stdout", "stderr", "truncated":
			q += `,` + k + `=?`
			args = append(args, v)
		}
	}
	q += ` WHERE id=? AND status=?`
	args = append(args, id, r.Status)
	if _, err := s.db.Exec(q, args...); err != nil {
		return nil, err
	}
	if err := s.auditLocked(actor, kind, action, id, r.Target, remote, detail); err != nil {
		return nil, err
	}
	return scanRequest(s.db.QueryRow(`SELECT `+reqCols+` FROM requests WHERE id=?`, id))
}

// ExpirePending marks pending requests past their deadline as expired.
func (s *Store) ExpirePending(now time.Time) (int, error) {
	rows, err := s.db.Query(`SELECT id FROM requests WHERE status=? AND expires_at>0 AND expires_at<?`,
		StatusPending, now.UnixMilli())
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	n := 0
	for _, id := range ids {
		if _, err := s.Transition(id, []string{StatusPending}, StatusExpired, nil,
			"gatekeep", "system", "request.expire", "", nil); err == nil {
			n++
		}
	}
	return n, nil
}
