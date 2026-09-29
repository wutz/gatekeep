package server

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wutz/gatekeep/internal/commands"
	"github.com/wutz/gatekeep/internal/config"
	"github.com/wutz/gatekeep/internal/executor"
	"github.com/wutz/gatekeep/internal/policy"
	"github.com/wutz/gatekeep/internal/store"
)

func testService(t *testing.T) *Service {
	t.Helper()
	pol, err := policy.Load("../../configs/policy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "gk.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{ApprovalTTL: 30, DefaultTarget: "local", Targets: []config.Target{
		{Name: "local", Transport: "local"}, {Name: "gpu", Transport: "local"},
	}}
	return &Service{Cfg: cfg, Policy: pol, Store: st, Exec: &executor.Executor{}, Events: NewHub()}
}

var (
	admin    = &config.Principal{Name: "alice", Kind: config.Human, Role: config.Admin}
	operator = &config.Principal{Name: "bob", Kind: config.Human, Role: config.Operator}
	agent    = &config.Principal{Name: "claude", Kind: config.Agent}
)

func TestCustomCommands(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	echo := &commands.Command{Name: "say", Template: "echo hello {{who}}", Level: policy.L0Read, Enabled: true,
		Params: []commands.Param{{Name: "who", Pattern: "[a-z]+"}}}
	if _, err := s.SaveCommand(operator, echo, ""); err == nil {
		t.Fatal("operator created a command")
	}
	if _, err := s.SaveCommand(admin, echo, ""); err != nil {
		t.Fatal(err)
	}

	// L0 command: agents run it directly, argv rendered from the template.
	r, err := s.RunCommand(ctx, agent, "say", "", map[string]string{"who": "world"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != store.StatusSucceeded || strings.TrimSpace(r.Stdout) != "hello world" || r.Command != "say" {
		t.Fatalf("got %+v", r)
	}
	if _, err := s.RunCommand(ctx, agent, "say", "", map[string]string{"who": "a;b"}, "", ""); err == nil {
		t.Fatal("injected value accepted")
	}

	// A command declared L0 whose rendered argv hits a locked rule is raised.
	rm := &commands.Command{Name: "wipe", Template: "rm -rf {{path}}", Level: policy.L0Read, Enabled: true,
		Params: []commands.Param{{Name: "path"}}, Targets: []string{"gpu"}}
	if _, err := s.SaveCommand(admin, rm, ""); err != nil {
		t.Fatal(err)
	}
	c, err := s.CheckCommand(agent, "wipe", "", map[string]string{"path": "/tmp/x"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Level != policy.L3Critical || c.Target != "gpu" || c.Outcome != policy.NeedApproval {
		t.Fatalf("floor not applied: %+v", c)
	}
	if _, err := s.RunCommand(ctx, agent, "wipe", "local", map[string]string{"path": "/tmp/x"}, "x", ""); err == nil {
		t.Fatal("disallowed target accepted")
	}
	r, err = s.RunCommand(ctx, agent, "wipe", "", map[string]string{"path": "/tmp/x"}, "cleanup", "")
	if err != nil || r.Status != store.StatusPending || r.Level != 3 {
		t.Fatalf("got %+v %v", r, err)
	}

	// Disabled commands are hidden from non-admins and cannot run.
	echo.Enabled = false
	if _, err := s.SaveCommand(admin, echo, ""); err != nil {
		t.Fatal(err)
	}
	if l, _ := s.ListCommands(agent); len(l.Commands) != 1 {
		t.Fatalf("agent sees %d commands", len(l.Commands))
	}
	if _, err := s.RunCommand(ctx, agent, "say", "", map[string]string{"who": "x"}, "", ""); err == nil {
		t.Fatal("disabled command ran")
	}
	if v, err := s.Store.Verify(); err != nil || !v.OK {
		t.Fatalf("audit chain: %+v %v", v, err)
	}
}
