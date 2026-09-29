package server

import (
	"context"
	"errors"

	"github.com/wutz/gatekeep/internal/commands"
	"github.com/wutz/gatekeep/internal/config"
	"github.com/wutz/gatekeep/internal/policy"
	"github.com/wutz/gatekeep/internal/store"
)

// Custom commands are admin-defined, parameterized command templates (e.g.
// restart-service = "systemctl restart {{service}}"). Anyone may run them by
// name; each run is an ordinary request whose level is the command's level,
// raised — never lowered — by the policy floor (locked rules, shell
// metacharacters), and which goes through the normal approval flow.

func (s *Service) validateCommand(c *commands.Command) error {
	if err := c.Validate(); err != nil {
		return errf(400, "%v", err)
	}
	for _, t := range c.Targets {
		if _, ok := s.Cfg.Target(t); !ok {
			return errf(400, "unknown target %q", t)
		}
	}
	return nil
}

func commandErr(err error, name string) error {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return errf(404, "custom command %q not found", name)
	case errors.Is(err, store.ErrDuplicate):
		return errf(409, "a command named %q already exists", name)
	}
	return err
}

// CommandList is what ListCommands returns.
type CommandList struct {
	Commands []commands.Command `json:"commands"`
	CanEdit  bool               `json:"can_edit"`
}

// ListCommands returns the custom commands p may see. Only admins see
// disabled commands; everyone else only sees those usable on a target they
// have access to.
func (s *Service) ListCommands(p *config.Principal) (*CommandList, error) {
	all, err := s.Store.ListCommands()
	if err != nil {
		return nil, err
	}
	edit := canManageRules(p)
	out := &CommandList{Commands: []commands.Command{}, CanEdit: edit}
	for _, c := range all {
		if edit || (c.Enabled && s.usableBy(p, &c)) {
			out.Commands = append(out.Commands, c)
		}
	}
	return out, nil
}

func (s *Service) usableBy(p *config.Principal, c *commands.Command) bool {
	if len(c.Targets) == 0 {
		for _, t := range s.Cfg.Targets {
			if p.CanUseTarget(t.Name) {
				return true
			}
		}
		return false
	}
	for _, t := range c.Targets {
		if p.CanUseTarget(t) {
			return true
		}
	}
	return false
}

// SaveCommand creates (ID == 0) or updates a custom command.
func (s *Service) SaveCommand(p *config.Principal, c *commands.Command, remote string) (*commands.Command, error) {
	if !canManageRules(p) {
		s.Store.Audit(p.Name, string(p.Kind), "command.forbidden", "", "", remote, map[string]any{"command": c})
		return nil, errf(403, "only admins may manage custom commands")
	}
	if err := s.validateCommand(c); err != nil {
		return nil, err
	}
	var err error
	if c.ID == 0 {
		err = s.Store.CreateCommand(c, p.Name, string(p.Kind), remote)
	} else {
		err = s.Store.UpdateCommand(c, p.Name, string(p.Kind), remote)
	}
	if err != nil {
		return nil, commandErr(err, c.Name)
	}
	s.Events.Publish("commands", "")
	return c, nil
}

// DeleteCommand removes a custom command. Past requests keep their argv.
func (s *Service) DeleteCommand(p *config.Principal, id int64, remote string) error {
	if !canManageRules(p) {
		s.Store.Audit(p.Name, string(p.Kind), "command.forbidden", "", "", remote, map[string]any{"delete": id})
		return errf(403, "only admins may manage custom commands")
	}
	if err := s.Store.DeleteCommand(id, p.Name, string(p.Kind), remote); err != nil {
		return commandErr(err, "")
	}
	s.Events.Publish("commands", "")
	return nil
}

// resolveCommand loads an enabled command, checks the target and renders argv.
func (s *Service) resolveCommand(name, target string, values map[string]string) (*commands.Command, string, []string, error) {
	c, err := s.Store.GetCommandByName(name)
	if err != nil {
		return nil, "", nil, commandErr(err, name)
	}
	if !c.Enabled {
		return nil, "", nil, errf(409, "custom command %q is disabled", name)
	}
	if target == "" {
		target = s.Cfg.DefaultTarget
		if len(c.Targets) > 0 && !c.AllowsTarget(target) {
			target = c.Targets[0]
		}
	}
	if !c.AllowsTarget(target) {
		return nil, "", nil, errf(400, "custom command %q is not allowed on target %q (allowed: %v)", name, target, c.Targets)
	}
	argv, err := c.Render(values)
	if err != nil {
		return nil, "", nil, errf(400, "%s: %v", name, err)
	}
	return c, target, argv, nil
}

// commandClassifier grades a rendered custom command: its declared level,
// unless the policy floor demands more.
func (s *Service) commandClassifier(c *commands.Command) func([]string, string) (policy.Decision, error) {
	return func(argv []string, target string) (policy.Decision, error) {
		d := policy.Decision{Level: c.Level, Rule: "command:" + c.Name}
		if f, ok := s.Policy.Floor(argv, target); ok && f.Level > d.Level {
			d = f
		}
		return d, nil
	}
}

// CheckCommand renders and classifies a custom command without running it.
func (s *Service) CheckCommand(p *config.Principal, name, target string, values map[string]string) (*ClassifyResult, error) {
	c, target, argv, err := s.resolveCommand(name, target, values)
	if err != nil {
		return nil, err
	}
	d, _ := s.commandClassifier(c)(argv, target)
	return &ClassifyResult{Argv: argv, Target: target, Level: d.Level, LevelS: d.Level.String(), Rule: d.Rule,
		Outcome: policy.Authorize(actor(p), d.Level)}, nil
}

// RunCommand submits a custom command as a request.
func (s *Service) RunCommand(ctx context.Context, p *config.Principal, name, target string, values map[string]string, reason, remote string) (*store.Request, error) {
	c, target, argv, err := s.resolveCommand(name, target, values)
	if err != nil {
		s.Store.Audit(p.Name, string(p.Kind), "command.invalid", "", target, remote,
			map[string]any{"command": name, "values": values, "error": err.Error()})
		return nil, err
	}
	return s.submit(ctx, p, target, argv, reason, remote, c.Name, s.commandClassifier(c))
}
