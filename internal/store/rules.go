package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/wutz/gatekeep/internal/policy"
)

// ErrDuplicate is returned when a custom rule name is already taken.
var ErrDuplicate = errors.New("duplicate name")

const ruleCols = `id,name,program,args,not_args,level,target,priority,enabled,note,created_by,created_at,updated_by,updated_at`

func scanRule(sc interface{ Scan(...any) error }) (*policy.CustomRule, error) {
	var r policy.CustomRule
	var en int
	err := sc.Scan(&r.ID, &r.Name, &r.Program, &r.Args, &r.NotArgs, &r.Level, &r.Target, &r.Priority, &en,
		&r.Note, &r.CreatedBy, &r.CreatedAt, &r.UpdatedBy, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	r.Enabled = en != 0
	return &r, nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func dupErr(err error) error {
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return ErrDuplicate
	}
	return err
}

// ListRules returns custom rules in evaluation order.
func (s *Store) ListRules() ([]policy.CustomRule, error) {
	rows, err := s.db.Query(`SELECT ` + ruleCols + ` FROM custom_rules ORDER BY priority, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []policy.CustomRule{}
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// GetRule loads one custom rule.
func (s *Store) GetRule(id int64) (*policy.CustomRule, error) {
	return scanRule(s.db.QueryRow(`SELECT `+ruleCols+` FROM custom_rules WHERE id=?`, id))
}

// CreateRule inserts r (already validated) and audits it.
func (s *Store) CreateRule(r *policy.CustomRule, actor, kind, remote string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UnixMilli()
	r.CreatedBy, r.CreatedAt, r.UpdatedBy, r.UpdatedAt = actor, now, actor, now
	res, err := s.db.Exec(`INSERT INTO custom_rules(name,program,args,not_args,level,target,priority,enabled,note,
		created_by,created_at,updated_by,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.Name, r.Program, r.Args, r.NotArgs, r.Level, r.Target, r.Priority, b2i(r.Enabled), r.Note,
		actor, now, actor, now)
	if err != nil {
		return dupErr(err)
	}
	r.ID, _ = res.LastInsertId()
	return s.auditLocked(actor, kind, "rule.create", "", r.Target, remote, map[string]any{"rule": r})
}

// UpdateRule replaces rule r.ID and audits the before/after values.
func (s *Store) UpdateRule(r *policy.CustomRule, actor, kind, remote string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	before, err := scanRule(s.db.QueryRow(`SELECT `+ruleCols+` FROM custom_rules WHERE id=?`, r.ID))
	if err != nil {
		return err
	}
	r.CreatedBy, r.CreatedAt = before.CreatedBy, before.CreatedAt
	r.UpdatedBy, r.UpdatedAt = actor, time.Now().UnixMilli()
	_, err = s.db.Exec(`UPDATE custom_rules SET name=?,program=?,args=?,not_args=?,level=?,target=?,priority=?,
		enabled=?,note=?,updated_by=?,updated_at=? WHERE id=?`,
		r.Name, r.Program, r.Args, r.NotArgs, r.Level, r.Target, r.Priority, b2i(r.Enabled), r.Note,
		r.UpdatedBy, r.UpdatedAt, r.ID)
	if err != nil {
		return dupErr(err)
	}
	return s.auditLocked(actor, kind, "rule.update", "", r.Target, remote, map[string]any{"before": before, "after": r})
}

// DeleteRule removes a rule and audits its last value.
func (s *Store) DeleteRule(id int64, actor, kind, remote string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	before, err := scanRule(s.db.QueryRow(`SELECT `+ruleCols+` FROM custom_rules WHERE id=?`, id))
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM custom_rules WHERE id=?`, id); err != nil {
		return err
	}
	return s.auditLocked(actor, kind, "rule.delete", "", before.Target, remote, map[string]any{"rule": before})
}
