package server

import (
	"errors"
	"sort"

	"github.com/wutz/gatekeep/internal/config"
	"github.com/wutz/gatekeep/internal/policy"
	"github.com/wutz/gatekeep/internal/store"
)

// Custom rules let admins re-grade commands at runtime. They are stored in
// the database, audited on every change, and hot-reloaded into the policy.

func canManageRules(p *config.Principal) bool {
	return p.Kind == config.Human && p.Role == config.Admin
}

// LoadRules pushes the stored custom rules into the live policy.
func (s *Service) LoadRules() error {
	rules, err := s.Store.ListRules()
	if err != nil {
		return err
	}
	return s.Policy.SetCustom(rules)
}

func (s *Service) validateRule(r *policy.CustomRule) error {
	if err := r.Validate(); err != nil {
		return errf(400, "%v", err)
	}
	if _, ok := s.Cfg.Target(r.Target); r.Target != "" && !ok {
		return errf(400, "unknown target %q", r.Target)
	}
	return nil
}

func ruleErr(err error, name string) error {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return errf(404, "rule not found")
	case errors.Is(err, store.ErrDuplicate):
		return errf(409, "a rule named %q already exists", name)
	}
	return err
}

// SaveRule creates (ID == 0) or updates a custom rule.
func (s *Service) SaveRule(p *config.Principal, r *policy.CustomRule, remote string) (*policy.CustomRule, error) {
	if !canManageRules(p) {
		s.Store.Audit(p.Name, string(p.Kind), "rule.forbidden", "", r.Target, remote, map[string]any{"rule": r})
		return nil, errf(403, "only admins may manage custom rules")
	}
	if err := s.validateRule(r); err != nil {
		return nil, err
	}
	var err error
	if r.ID == 0 {
		err = s.Store.CreateRule(r, p.Name, string(p.Kind), remote)
	} else {
		err = s.Store.UpdateRule(r, p.Name, string(p.Kind), remote)
	}
	if err != nil {
		return nil, ruleErr(err, r.Name)
	}
	if err := s.LoadRules(); err != nil {
		return nil, err
	}
	s.Events.Publish("rules", "")
	return r, nil
}

// DeleteRule removes a custom rule.
func (s *Service) DeleteRule(p *config.Principal, id int64, remote string) error {
	if !canManageRules(p) {
		s.Store.Audit(p.Name, string(p.Kind), "rule.forbidden", "", "", remote, map[string]any{"delete": id})
		return errf(403, "only admins may manage custom rules")
	}
	if err := s.Store.DeleteRule(id, p.Name, string(p.Kind), remote); err != nil {
		return ruleErr(err, "")
	}
	if err := s.LoadRules(); err != nil {
		return err
	}
	s.Events.Publish("rules", "")
	return nil
}

// RuleTest is one command's classification before and after a candidate rule.
type RuleTest struct {
	Command string          `json:"command"`
	Target  string          `json:"target"`
	Before  policy.Decision `json:"before"`
	After   policy.Decision `json:"after"`
	Error   string          `json:"error,omitempty"`
}

// TestRule previews how candidate (new, or an edit of an existing rule)
// would change the classification of the given commands, without saving.
func (s *Service) TestRule(p *config.Principal, candidate policy.CustomRule, target string, commands []string) ([]RuleTest, error) {
	if p.Kind != config.Human {
		return nil, errf(403, "this endpoint is for humans only")
	}
	if err := s.validateRule(&candidate); err != nil {
		return nil, err
	}
	rules, err := s.Store.ListRules()
	if err != nil {
		return nil, err
	}
	next := make([]policy.CustomRule, 0, len(rules)+1)
	for _, r := range rules {
		if r.ID != candidate.ID || candidate.ID == 0 {
			next = append(next, r)
		}
	}
	candidate.Enabled = true
	next = append(next, candidate)
	sort.SliceStable(next, func(i, j int) bool { return next[i].Priority < next[j].Priority })
	preview, err := s.Policy.WithCustom(next)
	if err != nil {
		return nil, errf(400, "%v", err)
	}
	if target == "" {
		target = candidate.Target
	}
	if target == "" {
		target = s.Cfg.DefaultTarget
	}
	out := []RuleTest{}
	for _, c := range commands {
		t := RuleTest{Command: c, Target: target}
		argv, err := ParseCommand(nil, c)
		if err == nil {
			t.Before, err = s.Policy.Classify(argv, target)
		}
		if err == nil {
			t.After, err = preview.Classify(argv, target)
		}
		if err != nil {
			t.Error = err.Error()
		}
		out = append(out, t)
	}
	return out, nil
}
