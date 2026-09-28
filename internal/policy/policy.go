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
// set, must NOT match.
type Rule struct {
	Name    string `yaml:"name" json:"name"`
	Program string `yaml:"program" json:"program"`
	Args    string `yaml:"args,omitempty" json:"args,omitempty"`
	NotArgs string `yaml:"not_args,omitempty" json:"not_args,omitempty"`
	Level   Level  `yaml:"level" json:"level"`
	globs   []string
	re      *regexp.Regexp
	notRe   *regexp.Regexp
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
}

// Decision is the classification result.
type Decision struct {
	Level Level  `json:"level"`
	Rule  string `json:"rule"`
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
		r := &p.Rules[i]
		if r.Program == "" {
			return nil, fmt.Errorf("rule %q: program required", r.Name)
		}
		r.globs = expandBraces(r.Program)
		for _, g := range r.globs {
			if _, err := filepath.Match(g, ""); err != nil {
				return nil, fmt.Errorf("rule %q: bad program glob: %w", r.Name, err)
			}
		}
		var err error
		if r.Args != "" {
			if r.re, err = regexp.Compile(r.Args); err != nil {
				return nil, fmt.Errorf("rule %q: args: %w", r.Name, err)
			}
		}
		if r.NotArgs != "" {
			if r.notRe, err = regexp.Compile(r.NotArgs); err != nil {
				return nil, fmt.Errorf("rule %q: not_args: %w", r.Name, err)
			}
		}
	}
	return p, nil
}

var shellMeta = regexp.MustCompile("[;&|`$<>]|\\$\\(")

// Classify returns the risk level for argv.
func (p *Policy) Classify(argv []string) (Decision, error) {
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
	for _, r := range p.Rules {
		if !r.matchProgram(prog) {
			continue
		}
		if r.re != nil && !r.re.MatchString(args) {
			continue
		}
		if r.notRe != nil && r.notRe.MatchString(args) {
			continue
		}
		return Decision{Level: r.Level, Rule: r.Name}, nil
	}
	return Decision{Level: p.DefaultLevel, Rule: "default"}, nil
}
