package commands

import (
	"reflect"
	"strings"
	"testing"
)

func mk(t *testing.T, tpl string, params ...Param) *Command {
	t.Helper()
	c := &Command{Name: "x", Template: tpl, Params: params, Level: 1}
	if err := c.Validate(); err != nil {
		t.Fatalf("validate %q: %v", tpl, err)
	}
	return c
}

func TestRender(t *testing.T) {
	c := mk(t, `systemctl restart {{service}}`, Param{Name: "service", Pattern: `kubelet|containerd`})
	argv, err := c.Render(map[string]string{"service": "kubelet"})
	if err != nil || !reflect.DeepEqual(argv, []string{"systemctl", "restart", "kubelet"}) {
		t.Fatalf("got %v %v", argv, err)
	}
	for _, bad := range []string{"sshd", "kubelet; rm -rf /", "kubelet containerd", ""} {
		if _, err := c.Render(map[string]string{"service": bad}); err == nil {
			t.Errorf("value %q accepted", bad)
		}
	}
	if _, err := c.Render(map[string]string{"service": "kubelet", "extra": "1"}); err == nil {
		t.Error("unknown param accepted")
	}

	// Default pattern: no leading dash (option injection), no spaces.
	d := mk(t, `journalctl -u {{unit}} -n {{lines}}`, Param{Name: "unit"}, Param{Name: "lines", Pattern: `[0-9]{1,4}`, Default: "200"})
	argv, err = d.Render(map[string]string{"unit": "kubelet.service"})
	if err != nil || strings.Join(argv, " ") != "journalctl -u kubelet.service -n 200" {
		t.Fatalf("got %v %v", argv, err)
	}
	for _, bad := range []string{"--help", "-x", "a b", "$(id)", "a`b`"} {
		if _, err := d.Render(map[string]string{"unit": bad}); err == nil {
			t.Errorf("default pattern accepted %q", bad)
		}
	}

	// Placeholder inside a token; optional empty param drops its element.
	o := mk(t, `kubectl -n {{ns}} rollout restart deploy/{{name}} {{extra}}`,
		Param{Name: "ns"}, Param{Name: "name"}, Param{Name: "extra", Optional: true})
	argv, err = o.Render(map[string]string{"ns": "gpu", "name": "dcgm"})
	if err != nil || !reflect.DeepEqual(argv, []string{"kubectl", "-n", "gpu", "rollout", "restart", "deploy/dcgm"}) {
		t.Fatalf("got %v %v", argv, err)
	}
}

func TestValidate(t *testing.T) {
	bad := []Command{
		{Name: "Bad Name", Template: "ls", Level: 0},
		{Name: "a", Template: "{{p}} x", Params: []Param{{Name: "p"}}},
		{Name: "a", Template: "ls {{p}}"},                                          // undeclared
		{Name: "a", Template: "ls", Params: []Param{{Name: "p"}}},                  // unused
		{Name: "a", Template: "ls {{p}}", Params: []Param{{Name: "p"}, {Name: "p"}}}, // dup
		{Name: "a", Template: "ls {{p}}", Params: []Param{{Name: "p", Pattern: "("}}},
		{Name: "a", Template: "ls {{p}}", Params: []Param{{Name: "p", Pattern: "[0-9]+", Default: "x"}}},
		{Name: "a", Template: "ls", Level: 4},
		{Name: "a", Template: `ls "unterminated`},
	}
	for i, c := range bad {
		if err := c.Validate(); err == nil {
			t.Errorf("#%d %+v: expected error", i, c)
		}
	}
}
