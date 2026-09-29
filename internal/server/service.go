package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/shlex"
	"github.com/wutz/gatekeep/internal/config"
	"github.com/wutz/gatekeep/internal/executor"
	"github.com/wutz/gatekeep/internal/policy"
	"github.com/wutz/gatekeep/internal/store"
)

// Service is the transport-independent core used by both the HTTP API and
// the MCP endpoint.
type Service struct {
	Cfg    *config.Config
	Policy *policy.Policy
	Store  *store.Store
	Exec   *executor.Executor
	Events *Hub
}

// Error with an HTTP-ish status code.
type Error struct {
	Code int
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

func errf(code int, f string, a ...any) error { return &Error{Code: code, Msg: fmt.Sprintf(f, a...)} }

func actor(p *config.Principal) policy.Actor {
	return policy.Actor{Name: p.Name, Agent: p.Kind == config.Agent, Role: string(p.Role)}
}

// ParseCommand accepts either argv or a single command string (split with
// POSIX-like rules, never passed to a shell).
func ParseCommand(argv []string, command string) ([]string, error) {
	if len(argv) > 0 {
		return argv, nil
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, errf(400, "command is required")
	}
	parts, err := shlex.Split(command)
	if err != nil {
		return nil, errf(400, "cannot parse command: %v", err)
	}
	return parts, nil
}

// ClassifyResult is returned by Check.
type ClassifyResult struct {
	Argv    []string       `json:"argv"`
	Target  string         `json:"target"`
	Level   policy.Level   `json:"level"`
	LevelS  string         `json:"level_name"`
	Rule    string         `json:"rule"`
	Custom  bool           `json:"custom,omitempty"`
	Outcome policy.Outcome `json:"outcome"`
}

// Check classifies without executing (dry run).
func (s *Service) Check(p *config.Principal, target string, argv []string) (*ClassifyResult, error) {
	if target == "" {
		target = s.Cfg.DefaultTarget
	}
	d, err := s.Policy.Classify(argv, target)
	if err != nil {
		return nil, errf(400, "%v", err)
	}
	return &ClassifyResult{Argv: argv, Target: target, Level: d.Level, LevelS: d.Level.String(), Rule: d.Rule,
		Custom: d.Custom, Outcome: policy.Authorize(actor(p), d.Level)}, nil
}

// Submit classifies argv, records it, and either executes immediately,
// queues it for approval, or denies it.
func (s *Service) Submit(ctx context.Context, p *config.Principal, target string, argv []string, reason, remote string) (*store.Request, error) {
	if target == "" {
		target = s.Cfg.DefaultTarget
	}
	t, ok := s.Cfg.Target(target)
	if !ok {
		return nil, errf(400, "unknown target %q (see list_targets)", target)
	}
	if !p.CanUseTarget(target) {
		s.Store.Audit(p.Name, string(p.Kind), "request.forbidden_target", "", target, remote, map[string]any{"argv": argv})
		return nil, errf(403, "principal %q may not use target %q", p.Name, target)
	}
	d, err := s.Policy.Classify(argv, target)
	if err != nil {
		return nil, errf(400, "%v", err)
	}
	outcome := policy.Authorize(actor(p), d.Level)
	r := &store.Request{
		ID: store.NewID(), Requester: p.Name, RequesterKind: string(p.Kind), Target: target,
		Argv: argv, Reason: reason, Level: int(d.Level), Rule: d.Rule,
	}
	switch outcome {
	case policy.Deny:
		r.Status = store.StatusDenied
	case policy.NeedApproval:
		if strings.TrimSpace(reason) == "" {
			return nil, errf(400, "this command is %s (rule %s) and needs human approval; a reason is required", d.Level, d.Rule)
		}
		r.Status = store.StatusPending
		r.ExpiresAt = time.Now().Add(time.Duration(s.Cfg.ApprovalTTL) * time.Minute).UnixMilli()
	case policy.Allow:
		r.Status = store.StatusApproved
	}
	if err := s.Store.CreateRequest(r, remote); err != nil {
		return nil, err
	}
	s.Events.Publish("request", r.ID)
	if outcome == policy.Allow {
		return s.execute(ctx, r.ID, t, p.Name, string(p.Kind), remote)
	}
	return r, nil
}

// execute runs an approved request.
func (s *Service) execute(ctx context.Context, id string, t config.Target, who, kind, remote string) (*store.Request, error) {
	r, err := s.Store.Transition(id, []string{store.StatusApproved}, store.StatusRunning,
		map[string]any{"started_at": time.Now().UnixMilli()}, who, kind, "request.execute", remote, nil)
	if err != nil {
		return r, err
	}
	s.Events.Publish("request", id)
	res := s.Exec.Run(ctx, t, r.Argv)
	status := store.StatusSucceeded
	if res.ExitCode != 0 {
		status = store.StatusFailed
	}
	trunc := 0
	if res.Truncated {
		trunc = 1
	}
	r, err = s.Store.Transition(id, []string{store.StatusRunning}, status, map[string]any{
		"finished_at": time.Now().UnixMilli(), "exit_code": res.ExitCode,
		"stdout": res.Stdout, "stderr": res.Stderr, "truncated": trunc,
	}, "gatekeep", "system", "request.finish", "", map[string]any{
		"exit_code": res.ExitCode, "duration_ms": res.Duration.Milliseconds(),
		"stdout_bytes": len(res.Stdout), "stderr_bytes": len(res.Stderr), "truncated": res.Truncated,
	})
	s.Events.Publish("request", id)
	return r, err
}

// Decide approves or rejects a pending request. Approved requests execute
// right away on behalf of the requester.
func (s *Service) Decide(ctx context.Context, p *config.Principal, id string, approve bool, note, remote string) (*store.Request, error) {
	r, err := s.Store.GetRequest(id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, errf(404, "request %s not found", id)
	}
	if err != nil {
		return nil, err
	}
	if !policy.CanApprove(actor(p), r.Requester, policy.Level(r.Level)) {
		s.Store.Audit(p.Name, string(p.Kind), "request.decide_forbidden", id, r.Target, remote, map[string]any{"approve": approve})
		return nil, errf(403, "%s may not decide this request (level %s, requester %s)", p.Name, policy.Level(r.Level), r.Requester)
	}
	if r.ExpiresAt > 0 && time.Now().UnixMilli() > r.ExpiresAt {
		s.Store.ExpirePending(time.Now())
		return nil, errf(409, "request %s has expired", id)
	}
	to, action := store.StatusRejected, "request.reject"
	if approve {
		to, action = store.StatusApproved, "request.approve"
	}
	r, err = s.Store.Transition(id, []string{store.StatusPending}, to, map[string]any{
		"approver": p.Name, "decided_at": time.Now().UnixMilli(), "decision_note": note,
	}, p.Name, string(p.Kind), action, remote, map[string]any{"note": note})
	if errors.Is(err, store.ErrConflict) {
		return r, errf(409, "request %s is %s, not pending", id, r.Status)
	}
	if err != nil {
		return nil, err
	}
	s.Events.Publish("request", id)
	if !approve {
		return r, nil
	}
	t, ok := s.Cfg.Target(r.Target)
	if !ok {
		return nil, errf(500, "target %q no longer configured", r.Target)
	}
	// Run detached from the approver's HTTP request so closing the browser
	// doesn't kill a command mid-flight.
	go s.execute(context.Background(), id, t, p.Name, string(p.Kind), remote)
	return r, nil
}

// Cancel withdraws a pending request (requester only).
func (s *Service) Cancel(p *config.Principal, id, remote string) (*store.Request, error) {
	r, err := s.Store.GetRequest(id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, errf(404, "request %s not found", id)
	}
	if err != nil {
		return nil, err
	}
	if r.Requester != p.Name {
		return nil, errf(403, "only the requester may cancel")
	}
	r, err = s.Store.Transition(id, []string{store.StatusPending}, store.StatusCancelled, nil,
		p.Name, string(p.Kind), "request.cancel", remote, nil)
	if errors.Is(err, store.ErrConflict) {
		return r, errf(409, "request %s is %s, not pending", id, r.Status)
	}
	s.Events.Publish("request", id)
	return r, err
}

// Get returns a request if p may see it. Agents only see their own.
func (s *Service) Get(p *config.Principal, id string) (*store.Request, error) {
	r, err := s.Store.GetRequest(id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, errf(404, "request %s not found", id)
	}
	if err != nil {
		return nil, err
	}
	if p.Kind == config.Agent && r.Requester != p.Name {
		return nil, errf(404, "request %s not found", id)
	}
	return r, nil
}

// Wait blocks until the request leaves pending/approved/running or timeout.
func (s *Service) Wait(ctx context.Context, p *config.Principal, id string, timeout time.Duration) (*store.Request, error) {
	deadline := time.After(timeout)
	sub := s.Events.Subscribe()
	defer s.Events.Unsubscribe(sub)
	for {
		r, err := s.Get(p, id)
		if err != nil {
			return nil, err
		}
		switch r.Status {
		case store.StatusPending, store.StatusApproved, store.StatusRunning:
		default:
			return r, nil
		}
		select {
		case <-ctx.Done():
			return r, nil
		case <-deadline:
			return r, nil
		case <-sub:
		case <-time.After(2 * time.Second):
		}
	}
}
