package policy

import (
	"strings"
	"testing"
)

func TestCustomRules(t *testing.T) {
	p, err := Load("../../configs/policy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	rules := []CustomRule{
		// downgrade: let agents restart kubelet without approval, only on gpu-105
		{ID: 1, Name: "kubelet-restart-gpu105", Program: "systemctl", Args: `^restart\s+kubelet$`, Level: L0Read, Target: "gpu-105", Enabled: true},
		// upgrade: reading any pod logs in prod namespace needs approval
		{ID: 2, Name: "prod-logs", Program: "kubectl", Args: `^logs\s.*-n\s+prod\b`, Level: L2High, Enabled: true},
		// attempt to downgrade a locked rule — must not take effect
		{ID: 3, Name: "rm-ok", Program: "rm", Level: L0Read, Enabled: true},
		// disabled rules are ignored
		{ID: 4, Name: "off", Program: "uptime", Level: L3Critical, Enabled: false},
	}
	if err := p.SetCustom(rules); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		cmd, target string
		want        Level
		custom      bool
	}{
		{"systemctl restart kubelet", "gpu-105", L0Read, true},
		{"systemctl restart kubelet", "gpu-106", L1Low, false},
		{"kubectl logs -n prod api-0", "", L2High, true},
		{"kubectl logs -n dev api-0", "", L0Read, false},
		{"rm -rf /data", "", L3Critical, false}, // locked wins
		{"rm foo", "", L0Read, true},            // not covered by a locked rule
		{"cat /etc/shadow", "", L2High, false},  // locked sensitive-files
		{"uptime", "", L0Read, false},
	}
	for _, c := range cases {
		d, err := p.Classify(strings.Fields(c.cmd), c.target)
		if err != nil {
			t.Fatal(err)
		}
		if d.Level != c.want || d.Custom != c.custom {
			t.Errorf("%q@%s: got %s custom=%v (rule %s), want %s custom=%v", c.cmd, c.target, d.Level, d.Custom, d.Rule, c.want, c.custom)
		}
	}
}

func TestCustomRuleValidate(t *testing.T) {
	bad := []CustomRule{
		{Name: "", Program: "ls"},
		{Name: "x", Program: ""},
		{Name: "x", Program: "ls", Level: 7},
		{Name: "x", Program: "ls", Args: "("},
		{Name: "x", Program: "*", Level: L0Read}, // catch-all downgrade
	}
	for _, r := range bad {
		if r.Validate() == nil {
			t.Errorf("expected %+v to be invalid", r)
		}
	}
	ok := CustomRule{Name: " x ", Program: "*", Level: L2High}
	if err := ok.Validate(); err != nil || ok.Name != "x" {
		t.Fatal(err, ok.Name)
	}
}
