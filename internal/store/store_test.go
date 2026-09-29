package store

import (
	"path/filepath"
	"testing"

	"github.com/wutz/gatekeep/internal/policy"
)

func TestAuditChain(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 5; i++ {
		if err := s.Audit("alice", "human", "login", "", "", "127.0.0.1", map[string]int{"i": i}); err != nil {
			t.Fatal(err)
		}
	}
	v, err := s.Verify()
	if err != nil || !v.OK || v.Count != 5 {
		t.Fatalf("verify: %+v %v", v, err)
	}
	// tamper with a record
	if _, err := s.db.Exec(`UPDATE audit SET actor='mallory' WHERE seq=3`); err != nil {
		t.Fatal(err)
	}
	v, _ = s.Verify()
	if v.OK || v.BrokenAt != 3 {
		t.Fatalf("tamper not detected: %+v", v)
	}
	s.db.Exec(`UPDATE audit SET actor='alice' WHERE seq=3`)
	s.db.Exec(`DELETE FROM audit WHERE seq=2`)
	v, _ = s.Verify()
	if v.OK || v.BrokenAt != 3 {
		t.Fatalf("deletion not detected: %+v", v)
	}
}

func TestTransition(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer s.Close()
	r := &Request{ID: NewID(), Requester: "bot", RequesterKind: "agent", Target: "local",
		Argv: []string{"kubectl", "delete", "pod", "x"}, Level: 1, Rule: "r", Status: StatusPending}
	if err := s.CreateRequest(r, ""); err != nil {
		t.Fatal(err)
	}
	got, err := s.Transition(r.ID, []string{StatusPending}, StatusApproved, map[string]any{"approver": "alice"},
		"alice", "human", "request.approve", "", nil)
	if err != nil || got.Approver != "alice" || got.Status != StatusApproved {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := s.Transition(r.ID, []string{StatusPending}, StatusRejected, nil, "bob", "human", "x", "", nil); err != ErrConflict {
		t.Fatalf("want conflict, got %v", err)
	}
	if v, _ := s.Verify(); !v.OK || v.Count != 2 {
		t.Fatalf("%+v", v)
	}
}

func TestRulesCRUD(t *testing.T) {
	s, err := Open(t.TempDir() + "/r.db")
	if err != nil {
		t.Fatal(err)
	}
	r := &policy.CustomRule{Name: "a", Program: "ls", Level: 1, Priority: 10, Enabled: true}
	if err := s.CreateRule(r, "alice", "human", ""); err != nil || r.ID == 0 {
		t.Fatal(err, r.ID)
	}
	if err := s.CreateRule(&policy.CustomRule{Name: "a", Program: "x"}, "alice", "human", ""); err != ErrDuplicate {
		t.Fatalf("want ErrDuplicate, got %v", err)
	}
	r.Level = 2
	if err := s.UpdateRule(r, "bob", "human", ""); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetRule(r.ID)
	if got.Level != 2 || got.CreatedBy != "alice" || got.UpdatedBy != "bob" {
		t.Fatalf("%+v", got)
	}
	if err := s.DeleteRule(r.ID, "alice", "human", ""); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.ListRules(); len(list) != 0 {
		t.Fatal(list)
	}
	ev, _ := s.ListAudit(AuditFilter{})
	if len(ev) != 3 {
		t.Fatalf("want 3 audit events, got %d", len(ev))
	}
	if v, _ := s.Verify(); !v.OK {
		t.Fatal(v)
	}
}
