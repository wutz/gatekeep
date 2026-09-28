package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// AuditEvent is one append-only audit record. Each record's Hash covers its
// content and the previous record's hash, so any edit or deletion in the
// middle of the log breaks the chain and is detected by Verify.
type AuditEvent struct {
	Seq       int64           `json:"seq"`
	TS        int64           `json:"ts"`
	Actor     string          `json:"actor"`
	ActorKind string          `json:"actor_kind"`
	Action    string          `json:"action"`
	RequestID string          `json:"request_id,omitempty"`
	Target    string          `json:"target,omitempty"`
	Detail    json.RawMessage `json:"detail,omitempty"`
	Remote    string          `json:"remote,omitempty"`
	PrevHash  string          `json:"prev_hash"`
	Hash      string          `json:"hash"`
}

func (e *AuditEvent) computeHash() string {
	h := sha256.New()
	fmt.Fprintf(h, "%d|%d|%s|%s|%s|%s|%s|%s|%s|%s",
		e.Seq, e.TS, e.Actor, e.ActorKind, e.Action, e.RequestID, e.Target, e.Detail, e.Remote, e.PrevHash)
	return hex.EncodeToString(h.Sum(nil))
}

// Audit appends an event. detail is marshaled to JSON.
func (s *Store) Audit(actor, kind, action, requestID, target, remote string, detail any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.auditLocked(actor, kind, action, requestID, target, remote, detail)
}

func (s *Store) auditLocked(actor, kind, action, requestID, target, remote string, detail any) error {
	d := []byte("{}")
	if detail != nil {
		var err error
		if d, err = json.Marshal(detail); err != nil {
			return err
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var prevSeq int64
	prev := strings.Repeat("0", 64)
	err = tx.QueryRow(`SELECT seq, hash FROM audit ORDER BY seq DESC LIMIT 1`).Scan(&prevSeq, &prev)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	e := AuditEvent{
		Seq: prevSeq + 1, TS: time.Now().UnixMilli(), Actor: actor, ActorKind: kind,
		Action: action, RequestID: requestID, Target: target, Detail: d, Remote: remote, PrevHash: prev,
	}
	e.Hash = e.computeHash()
	_, err = tx.Exec(`INSERT INTO audit(seq,ts,actor,actor_kind,action,request_id,target,detail,remote,prev_hash,hash)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		e.Seq, e.TS, e.Actor, e.ActorKind, e.Action, e.RequestID, e.Target, string(e.Detail), e.Remote, e.PrevHash, e.Hash)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// AuditFilter narrows ListAudit.
type AuditFilter struct {
	Actor     string
	Action    string
	RequestID string
	Before    int64 // seq; 0 = latest
	Limit     int
}

// ListAudit returns events newest first.
func (s *Store) ListAudit(f AuditFilter) ([]AuditEvent, error) {
	q := `SELECT seq,ts,actor,actor_kind,action,request_id,target,detail,remote,prev_hash,hash FROM audit WHERE 1=1`
	var args []any
	if f.Actor != "" {
		q += ` AND actor=?`
		args = append(args, f.Actor)
	}
	if f.Action != "" {
		q += ` AND action=?`
		args = append(args, f.Action)
	}
	if f.RequestID != "" {
		q += ` AND request_id=?`
		args = append(args, f.RequestID)
	}
	if f.Before > 0 {
		q += ` AND seq<?`
		args = append(args, f.Before)
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	q += ` ORDER BY seq DESC LIMIT ?`
	args = append(args, f.Limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEvent{}
	for rows.Next() {
		var e AuditEvent
		var d string
		if err := rows.Scan(&e.Seq, &e.TS, &e.Actor, &e.ActorKind, &e.Action, &e.RequestID, &e.Target, &d, &e.Remote, &e.PrevHash, &e.Hash); err != nil {
			return nil, err
		}
		e.Detail = json.RawMessage(d)
		out = append(out, e)
	}
	return out, rows.Err()
}

// VerifyResult reports audit chain integrity.
type VerifyResult struct {
	OK       bool   `json:"ok"`
	Count    int64  `json:"count"`
	BrokenAt int64  `json:"broken_at,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// Verify walks the whole chain and checks every hash link.
func (s *Store) Verify() (VerifyResult, error) {
	rows, err := s.db.Query(`SELECT seq,ts,actor,actor_kind,action,request_id,target,detail,remote,prev_hash,hash FROM audit ORDER BY seq`)
	if err != nil {
		return VerifyResult{}, err
	}
	defer rows.Close()
	prev := strings.Repeat("0", 64)
	var n, expectSeq int64 = 0, 1
	for rows.Next() {
		var e AuditEvent
		var d string
		if err := rows.Scan(&e.Seq, &e.TS, &e.Actor, &e.ActorKind, &e.Action, &e.RequestID, &e.Target, &d, &e.Remote, &e.PrevHash, &e.Hash); err != nil {
			return VerifyResult{}, err
		}
		e.Detail = json.RawMessage(d)
		switch {
		case e.Seq != expectSeq:
			return VerifyResult{Count: n, BrokenAt: e.Seq, Reason: fmt.Sprintf("missing record(s) before seq %d", e.Seq)}, nil
		case e.PrevHash != prev:
			return VerifyResult{Count: n, BrokenAt: e.Seq, Reason: "prev_hash mismatch"}, nil
		case e.computeHash() != e.Hash:
			return VerifyResult{Count: n, BrokenAt: e.Seq, Reason: "content hash mismatch (record modified)"}, nil
		}
		prev = e.Hash
		expectSeq++
		n++
	}
	return VerifyResult{OK: true, Count: n}, rows.Err()
}
