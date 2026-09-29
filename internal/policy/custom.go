package policy

import (
	"fmt"
	"strings"
)

// CustomRule is a user-defined classification rule managed at runtime (stored
// in the database, edited from the console / API / gk CLI). Custom rules are
// evaluated after locked built-in rules and before all other built-in rules,
// so operators can raise or lower the level of specific commands without
// editing policy.yaml — but can never override a locked rule.
type CustomRule struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Program  string `json:"program"`
	Args     string `json:"args,omitempty"`
	NotArgs  string `json:"not_args,omitempty"`
	Level    Level  `json:"level"`
	Target   string `json:"target,omitempty"` // empty = all targets
	Priority int    `json:"priority"`         // lower is evaluated first
	Enabled  bool   `json:"enabled"`
	Note     string `json:"note,omitempty"`
	// Bookkeeping, set by the store.
	CreatedBy string `json:"created_by,omitempty"`
	CreatedAt int64  `json:"created_at,omitempty"`
	UpdatedBy string `json:"updated_by,omitempty"`
	UpdatedAt int64  `json:"updated_at,omitempty"`
}

// Validate normalizes r and checks that it compiles.
func (r *CustomRule) Validate() error {
	r.Name = strings.TrimSpace(r.Name)
	r.Program = strings.TrimSpace(r.Program)
	r.Target = strings.TrimSpace(r.Target)
	if r.Name == "" {
		return fmt.Errorf("name is required")
	}
	if r.Level < L0Read || r.Level > L3Critical {
		return fmt.Errorf("level must be 0-3")
	}
	// A bare catch-all would silently turn every unknown command into
	// something agents can run; require it to be at least high risk.
	if strings.Trim(r.Program, "*") == "" && r.Args == "" && r.Level < L2High {
		return fmt.Errorf("a catch-all program %q without args must be level 2 or higher", r.Program)
	}
	_, err := r.compile()
	return err
}

func (r *CustomRule) compile() (Rule, error) {
	c := Rule{Name: r.Name, Program: r.Program, Args: r.Args, NotArgs: r.NotArgs, Level: r.Level}
	if err := c.compile(); err != nil {
		return Rule{}, err
	}
	c.custom = true
	c.id = r.ID
	c.target = r.Target
	return c, nil
}

// SetCustom replaces the active custom rules. Rules must already be sorted by
// priority; disabled rules are skipped. Safe for concurrent use with Classify.
func (p *Policy) SetCustom(rules []CustomRule) error {
	out := make([]Rule, 0, len(rules))
	for i := range rules {
		if !rules[i].Enabled {
			continue
		}
		c, err := rules[i].compile()
		if err != nil {
			return err
		}
		out = append(out, c)
	}
	p.custom.Store(&out)
	return nil
}

// WithCustom returns a copy of p (sharing its built-in rules) whose custom
// rules are replaced by rules. Used to preview a rule change before saving.
func (p *Policy) WithCustom(rules []CustomRule) (*Policy, error) {
	q := &Policy{DefaultLevel: p.DefaultLevel, Rules: p.Rules, RejectMeta: p.RejectMeta}
	return q, q.SetCustom(rules)
}
