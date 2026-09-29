// Package commands implements user-defined (custom) commands: named,
// parameterized command templates that admins publish so humans and agents
// can run vetted operations by name, e.g.
//
//	name: restart-service
//	template: systemctl restart {{service}}
//	params: [{name: service, pattern: '[a-z0-9@._-]+'}]
//
// A template is split into argv once (POSIX-like quoting, no shell). Each
// parameter value is substituted inside a single argv element and must fully
// match its pattern, so values can never add arguments, options or shell
// syntax.
package commands

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/google/shlex"
	"github.com/wutz/gatekeep/internal/policy"
)

// DefaultPattern is used when a parameter declares no pattern: a plain word
// that cannot start with "-" (so it cannot be an option).
const DefaultPattern = `[A-Za-z0-9_.:@/][A-Za-z0-9_.:@/=+-]*`

var (
	nameRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	paramRe = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,31}$`)
	placeRe = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_]+)\s*\}\}`)
)

// Param is one template parameter.
type Param struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Pattern     string `json:"pattern,omitempty"` // full-match regexp; empty = DefaultPattern
	Default     string `json:"default,omitempty"`
	Optional    bool   `json:"optional,omitempty"` // if empty, the argv element holding it is dropped
}

// Command is a user-defined command.
type Command struct {
	ID          int64        `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	Template    string       `json:"template"`
	Params      []Param      `json:"params"`
	Level       policy.Level `json:"level"`
	Targets     []string     `json:"targets"` // empty = all targets
	Enabled     bool         `json:"enabled"`
	CreatedBy   string       `json:"created_by,omitempty"`
	CreatedAt   int64        `json:"created_at,omitempty"`
	UpdatedBy   string       `json:"updated_by,omitempty"`
	UpdatedAt   int64        `json:"updated_at,omitempty"`
}

// EffectivePattern is Pattern, or DefaultPattern when unset.
func (p Param) EffectivePattern() string {
	if p.Pattern == "" {
		return DefaultPattern
	}
	return p.Pattern
}

func (p Param) re() (*regexp.Regexp, error) {
	return regexp.Compile(`^(?:` + p.EffectivePattern() + `)$`)
}

// Validate normalizes c and checks the template and parameters.
func (c *Command) Validate() error {
	c.Name = strings.TrimSpace(c.Name)
	c.Template = strings.TrimSpace(c.Template)
	if !nameRe.MatchString(c.Name) {
		return fmt.Errorf("name must be lowercase letters, digits, '-' or '_' (max 64)")
	}
	if c.Level < policy.L0Read || c.Level > policy.L3Critical {
		return fmt.Errorf("level must be 0-3")
	}
	toks, err := shlex.Split(c.Template)
	if err != nil {
		return fmt.Errorf("template: %v", err)
	}
	if len(toks) == 0 {
		return fmt.Errorf("template is required")
	}
	if placeRe.MatchString(toks[0]) {
		return fmt.Errorf("template: the program (first word) cannot be a parameter")
	}
	declared := map[string]*Param{}
	for i := range c.Params {
		p := &c.Params[i]
		p.Name = strings.TrimSpace(p.Name)
		if !paramRe.MatchString(p.Name) {
			return fmt.Errorf("param %q: name must match [a-z_][a-z0-9_]*", p.Name)
		}
		if declared[p.Name] != nil {
			return fmt.Errorf("param %q declared twice", p.Name)
		}
		re, err := p.re()
		if err != nil {
			return fmt.Errorf("param %q: pattern: %v", p.Name, err)
		}
		if p.Default != "" && !re.MatchString(p.Default) {
			return fmt.Errorf("param %q: default %q does not match its pattern", p.Name, p.Default)
		}
		declared[p.Name] = p
	}
	used := map[string]bool{}
	for _, t := range toks {
		for _, m := range placeRe.FindAllStringSubmatch(t, -1) {
			if declared[m[1]] == nil {
				return fmt.Errorf("template uses undeclared param {{%s}}", m[1])
			}
			used[m[1]] = true
		}
	}
	for n := range declared {
		if !used[n] {
			return fmt.Errorf("param %q is not used in the template", n)
		}
	}
	if c.Params == nil {
		c.Params = []Param{}
	}
	if c.Targets == nil {
		c.Targets = []string{}
	}
	return nil
}

// AllowsTarget reports whether c may run on target.
func (c *Command) AllowsTarget(target string) bool {
	if len(c.Targets) == 0 {
		return true
	}
	for _, t := range c.Targets {
		if t == target {
			return true
		}
	}
	return false
}

// Render substitutes values into the template and returns argv. Unknown
// value keys, missing required values and pattern mismatches are errors.
func (c *Command) Render(values map[string]string) ([]string, error) {
	toks, err := shlex.Split(c.Template)
	if err != nil {
		return nil, err
	}
	byName := map[string]Param{}
	for _, p := range c.Params {
		byName[p.Name] = p
	}
	for k := range values {
		if _, ok := byName[k]; !ok {
			return nil, fmt.Errorf("unknown param %q", k)
		}
	}
	final := map[string]string{}
	for _, p := range c.Params {
		v, ok := values[p.Name]
		if !ok || v == "" {
			v = p.Default
		}
		if v == "" {
			if !p.Optional {
				return nil, fmt.Errorf("param %q is required", p.Name)
			}
		} else {
			re, err := p.re()
			if err != nil {
				return nil, err
			}
			if !re.MatchString(v) {
				return nil, fmt.Errorf("param %q: value %q does not match %s", p.Name, v, p.EffectivePattern())
			}
		}
		final[p.Name] = v
	}
	argv := make([]string, 0, len(toks))
	for _, t := range toks {
		empty := false
		out := placeRe.ReplaceAllStringFunc(t, func(m string) string {
			v := final[placeRe.FindStringSubmatch(m)[1]]
			if v == "" {
				empty = true
			}
			return v
		})
		if empty {
			continue // optional param left blank: drop the whole element (e.g. "--since={{since}}")
		}
		argv = append(argv, out)
	}
	return argv, nil
}
