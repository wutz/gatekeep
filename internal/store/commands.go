package store

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/wutz/gatekeep/internal/commands"
)

const cmdCols = `id,name,description,template,params,level,targets,enabled,created_by,created_at,updated_by,updated_at`

func scanCommand(sc interface{ Scan(...any) error }) (*commands.Command, error) {
	var c commands.Command
	var params, targets string
	var en int
	err := sc.Scan(&c.ID, &c.Name, &c.Description, &c.Template, &params, &c.Level, &targets, &en,
		&c.CreatedBy, &c.CreatedAt, &c.UpdatedBy, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.Enabled = en != 0
	_ = json.Unmarshal([]byte(params), &c.Params)
	_ = json.Unmarshal([]byte(targets), &c.Targets)
	return &c, nil
}

// ListCommands returns all custom commands ordered by name.
func (s *Store) ListCommands() ([]commands.Command, error) {
	rows, err := s.db.Query(`SELECT ` + cmdCols + ` FROM custom_commands ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []commands.Command{}
	for rows.Next() {
		c, err := scanCommand(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// GetCommandByName loads a custom command by name.
func (s *Store) GetCommandByName(name string) (*commands.Command, error) {
	return scanCommand(s.db.QueryRow(`SELECT `+cmdCols+` FROM custom_commands WHERE name=?`, name))
}

// CreateCommand inserts c (already validated) and audits it.
func (s *Store) CreateCommand(c *commands.Command, actor, kind, remote string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UnixMilli()
	c.CreatedBy, c.CreatedAt, c.UpdatedBy, c.UpdatedAt = actor, now, actor, now
	params, _ := json.Marshal(c.Params)
	targets, _ := json.Marshal(c.Targets)
	res, err := s.db.Exec(`INSERT INTO custom_commands(name,description,template,params,level,targets,enabled,
		created_by,created_at,updated_by,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		c.Name, c.Description, c.Template, string(params), c.Level, string(targets), b2i(c.Enabled), actor, now, actor, now)
	if err != nil {
		return dupErr(err)
	}
	c.ID, _ = res.LastInsertId()
	return s.auditLocked(actor, kind, "command.create", "", "", remote, map[string]any{"command": c})
}

// UpdateCommand replaces command c.ID and audits the before/after values.
func (s *Store) UpdateCommand(c *commands.Command, actor, kind, remote string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	before, err := scanCommand(s.db.QueryRow(`SELECT `+cmdCols+` FROM custom_commands WHERE id=?`, c.ID))
	if err != nil {
		return err
	}
	c.CreatedBy, c.CreatedAt = before.CreatedBy, before.CreatedAt
	c.UpdatedBy, c.UpdatedAt = actor, time.Now().UnixMilli()
	params, _ := json.Marshal(c.Params)
	targets, _ := json.Marshal(c.Targets)
	_, err = s.db.Exec(`UPDATE custom_commands SET name=?,description=?,template=?,params=?,level=?,targets=?,
		enabled=?,updated_by=?,updated_at=? WHERE id=?`,
		c.Name, c.Description, c.Template, string(params), c.Level, string(targets), b2i(c.Enabled),
		c.UpdatedBy, c.UpdatedAt, c.ID)
	if err != nil {
		return dupErr(err)
	}
	return s.auditLocked(actor, kind, "command.update", "", "", remote, map[string]any{"before": before, "after": c})
}

// DeleteCommand removes a custom command and audits its last value.
func (s *Store) DeleteCommand(id int64, actor, kind, remote string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	before, err := scanCommand(s.db.QueryRow(`SELECT `+cmdCols+` FROM custom_commands WHERE id=?`, id))
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM custom_commands WHERE id=?`, id); err != nil {
		return err
	}
	return s.auditLocked(actor, kind, "command.delete", "", "", remote, map[string]any{"command": before})
}
