// Package config loads the gatekeep server configuration: principals (humans
// and agents), their credentials, and the targets commands may run on.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Kind distinguishes humans from agents.
type Kind string

const (
	Human Kind = "human"
	Agent Kind = "agent"
)

// Role applies to humans and controls which levels they may run and approve.
type Role string

const (
	Viewer   Role = "viewer"   // L0 only, cannot approve
	Operator Role = "operator" // runs L0-L1 directly, may approve up to L2
	Admin    Role = "admin"    // runs up to L2 directly, may approve anything
)

// Principal is an authenticated identity.
type Principal struct {
	Name string `yaml:"name" json:"name"`
	Kind Kind   `yaml:"kind" json:"kind"`
	Role Role   `yaml:"role,omitempty" json:"role,omitempty"`
	// TokenSHA256 is the hex sha256 of the bearer token. Plain Token is
	// accepted for development only.
	TokenSHA256 string `yaml:"token_sha256,omitempty" json:"-"`
	Token       string `yaml:"token,omitempty" json:"-"`
	// Targets restricts which targets this principal may use; empty = all.
	Targets []string `yaml:"targets,omitempty" json:"targets,omitempty"`
}

// Target is a place commands run. Transport "local" executes on the gatekeep
// host; "ssh" runs `ssh <host> -- <argv>` using the server's ssh config.
type Target struct {
	Name        string   `yaml:"name" json:"name"`
	Transport   string   `yaml:"transport" json:"transport"`
	Host        string   `yaml:"host,omitempty" json:"host,omitempty"`
	User        string   `yaml:"user,omitempty" json:"user,omitempty"`
	Labels      []string `yaml:"labels,omitempty" json:"labels,omitempty"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
}

// Config is the server configuration.
type Config struct {
	Listen      string `yaml:"listen"`
	DB          string `yaml:"db"`
	Policy      string `yaml:"policy"`
	WebDir      string `yaml:"web_dir"`
	ExecTimeout int    `yaml:"exec_timeout_seconds"`
	MaxOutput   int    `yaml:"max_output_bytes"`
	ApprovalTTL int    `yaml:"approval_ttl_minutes"`
	// DefaultTarget is used when a request names no target.
	DefaultTarget string      `yaml:"default_target"`
	Principals    []Principal `yaml:"principals"`
	Targets       []Target    `yaml:"targets"`
}

// Load reads and validates a config file.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c := &Config{
		Listen:      "127.0.0.1:8740",
		DB:          "gatekeep.db",
		Policy:      "configs/policy.yaml",
		ExecTimeout: 60,
		MaxOutput:   1 << 20,
		ApprovalTTL: 60,
	}
	if err := yaml.Unmarshal(b, c); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for i := range c.Principals {
		p := &c.Principals[i]
		if p.Name == "" || seen[p.Name] {
			return nil, fmt.Errorf("principal %d: empty or duplicate name %q", i, p.Name)
		}
		seen[p.Name] = true
		switch p.Kind {
		case Human:
			if p.Role == "" {
				p.Role = Viewer
			}
		case Agent:
			p.Role = ""
		default:
			return nil, fmt.Errorf("principal %q: kind must be human or agent", p.Name)
		}
		if p.TokenSHA256 == "" && p.Token == "" {
			return nil, fmt.Errorf("principal %q: token or token_sha256 required", p.Name)
		}
		if p.TokenSHA256 == "" {
			p.TokenSHA256 = HashToken(p.Token)
		}
		p.Token = ""
	}
	tseen := map[string]bool{}
	for _, t := range c.Targets {
		if t.Name == "" || tseen[t.Name] {
			return nil, fmt.Errorf("target: empty or duplicate name %q", t.Name)
		}
		tseen[t.Name] = true
		if t.Transport != "local" && t.Transport != "ssh" {
			return nil, fmt.Errorf("target %q: transport must be local or ssh", t.Name)
		}
		if t.Transport == "ssh" && t.Host == "" {
			return nil, fmt.Errorf("target %q: host required for ssh", t.Name)
		}
	}
	if c.DefaultTarget == "" && len(c.Targets) == 1 {
		c.DefaultTarget = c.Targets[0].Name
	}
	if _, ok := c.Target(c.DefaultTarget); c.DefaultTarget != "" && !ok {
		return nil, fmt.Errorf("default_target %q is not a configured target", c.DefaultTarget)
	}
	return c, nil
}

// HashToken returns the hex sha256 of a token.
func HashToken(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

// Target returns a target by name.
func (c *Config) Target(name string) (Target, bool) {
	for _, t := range c.Targets {
		if t.Name == name {
			return t, true
		}
	}
	return Target{}, false
}

// CanUseTarget reports whether p may run commands on target.
func (p Principal) CanUseTarget(target string) bool {
	if len(p.Targets) == 0 {
		return true
	}
	for _, t := range p.Targets {
		if t == target || t == "*" {
			return true
		}
	}
	return false
}
