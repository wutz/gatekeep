// Package policy classifies commands into risk levels.
//
// Every command is tokenized (no shell interpretation) and matched against an
// ordered list of rules. The first matching rule wins. Commands that match no
// rule fall back to DefaultLevel (high risk by default) so unknown operations
// are never silently treated as read-only.
package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"

	"gopkg.in/yaml.v3"
)

// Level is an operation risk level.
type Level int

const (
	L0Read     Level = 0 // read-only: status, logs, get/describe
	L1Low      Level = 1 // low risk, reversible: restart a pod, cordon
	L2High     Level = 2 // high risk: delete resources, scale, config change
	L3Critical Level = 3 // critical / destructive: data loss, cluster-wide
)

func (l Level) String() string {
	switch l {
	case L0Read:
		return "L0-read"
	case L1Low:
		return "L1-low"
	case L2High:
		return "L2-high"
	case L3Critical:
		return "L3-critical"
	}
	return fmt.Sprintf("L%d", int(l))
}

// Rule matches a command. Program is a glob matched against the program's base
// name (e.g. "kubectl", "mmls*", "*", "{free,df}"); Args is a regular
// expression matched against the space-joined argument string, and NotArgs, if
// set, must NOT match. Locked rules are evaluated before custom rules, so
// runtime customization can never downgrade them.
type Rule struct {
	Name    string `yaml:"name" json:"name"`
	Program string `yaml:"program" json:"program"`
	Args    string `yaml:"args,omitempty" json:"args,omitempty"`
	NotArgs string `yaml:"not_args,omitempty" json:"not_args,omitempty"`
	Level   Level  `yaml:"level" json:"level"`
	Locked  bool   `yaml:"locked,omitempty" json:"locked,omitempty"`
	globs   []string
	re      *regexp.Regexp
	notRe   *regexp.Regexp
	custom  bool
	id      int64
	target  string
}

func (r *Rule) compile() error {
	if r.Program == "" {
		return fmt.Errorf("rule %q: program required", r.Name)
	}
	r.globs = expandBraces(r.Program)
	for _, g := range r.globs {
		if _, err := filepath.Match(g, ""); err != nil {
			return fmt.Errorf("rule %q: bad program glob: %w", r.Name, err)
		}
	}
	var err error
	if r.Args != "" {
		if r.re, err = regexp.Compile(r.Args); err != nil {
			return fmt.Errorf("rule %q: args: %w", r.Name, err)
		}
	}
	if r.NotArgs != "" {
		if r.notRe, err = regexp.Compile(r.NotArgs); err != nil {
			return fmt.Errorf("rule %q: not_args: %w", r.Name, err)
		}
	}
	return nil
}

func (r *Rule) match(prog, args, target string) bool {
	if r.target != "" && r.target != target {
		return false
	}
	if !r.matchProgram(prog) {
		return false
	}
	if r.re != nil && !r.re.MatchString(args) {
		return false
	}
	return r.notRe == nil || !r.notRe.MatchString(args)
}

// expandBraces expands a single top-level "{a,b,c}" group.
func expandBraces(s string) []string {
	i := strings.IndexByte(s, '{')
	j := strings.IndexByte(s, '}')
	if i < 0 || j < i {
		return []string{s}
	}
	var out []string
	for _, alt := range strings.Split(s[i+1:j], ",") {
		out = append(out, expandBraces(s[:i]+strings.TrimSpace(alt)+s[j+1:])...)
	}
	return out
}

func (r *Rule) matchProgram(prog string) bool {
	for _, g := range r.globs {
		if ok, _ := filepath.Match(g, prog); ok {
			return true
		}
	}
	return false
}

// Policy is an ordered rule set.
type Policy struct {
	DefaultLevel Level  `yaml:"default_level" json:"default_level"`
	Rules        []Rule `yaml:"rules" json:"rules"`
	// Forbidden shell metacharacters; commands are exec'd directly, so these
	// only indicate an attempt to smuggle a pipeline/redirect.
	RejectMeta bool `yaml:"reject_shell_meta" json:"reject_shell_meta"`
	// custom holds the runtime (database-backed) rules; see SetCustom.
	custom atomic.Pointer[[]Rule]
}

// Decision is the classification result. Custom is set when a user-defined
// rule (CustomID) decided the level.
type Decision struct {
	Level    Level  `json:"level"`
	Rule     string `json:"rule"`
	Custom   bool   `json:"custom,omitempty"`
	CustomID int64  `json:"custom_id,omitempty"`
}

// Load reads a policy file.
func Load(path string) (*Policy, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

// Parse parses YAML policy content.
func Parse(b []byte) (*Policy, error) {
	p := &Policy{DefaultLevel: L2High, RejectMeta: true}
	if err := yaml.Unmarshal(b, p); err != nil {
		return nil, err
	}
	for i := range p.Rules {
		if err := p.Rules[i].compile(); err != nil {
			return nil, err
		}
	}
	return p, nil
}

var shellMeta = regexp.MustCompile("[;&|`$<>]|\\$\\(")

// Classify returns the risk level for argv on target. Evaluation order:
// shell metacharacters, locked built-in rules, custom rules (by priority),
// the remaining built-in rules, then DefaultLevel.
func (p *Policy) Classify(argv []string, target string) (Decision, error) {
	if len(argv) == 0 {
		return Decision{}, fmt.Errorf("empty command")
	}
	if p.RejectMeta {
		for _, a := range argv {
			if shellMeta.MatchString(a) {
				return Decision{Level: L3Critical, Rule: "shell-metacharacter"}, nil
			}
		}
	}
	prog := filepath.Base(argv[0])
	args := strings.Join(argv[1:], " ")
	for i := range p.Rules {
		if r := &p.Rules[i]; r.Locked && r.match(prog, args, target) {
			return Decision{Level: r.Level, Rule: r.Name}, nil
		}
	}
	if c := p.custom.Load(); c != nil {
		for i := range *c {
			if r := &(*c)[i]; r.match(prog, args, target) {
				return Decision{Level: r.Level, Rule: "custom:" + r.Name, Custom: true, CustomID: r.id}, nil
			}
		}
	}
	for i := range p.Rules {
		if r := &p.Rules[i]; !r.Locked && r.match(prog, args, target) {
			return Decision{Level: r.Level, Rule: r.Name}, nil
		}
	}
	return Decision{Level: p.DefaultLevel, Rule: "default"}, nil
}
