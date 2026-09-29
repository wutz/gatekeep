package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
)

type param struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Pattern     string `json:"pattern,omitempty"`
	Default     string `json:"default,omitempty"`
	Optional    bool   `json:"optional,omitempty"`
}

type customCommand struct {
	ID          int64    `json:"id,omitempty"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Template    string   `json:"template"`
	Params      []param  `json:"params"`
	Level       int      `json:"level"`
	Targets     []string `json:"targets"`
	Enabled     bool     `json:"enabled"`
	UpdatedBy   string   `json:"updated_by,omitempty"`
}

// runCustom runs a custom command: gk run restart-service service=kubelet
func runCustom(args []string, target, reason string, wait int) {
	if len(args) == 0 {
		die(fmt.Errorf("usage: gk [-t target] [-r reason] [--wait N] run <name> [key=value...] [--dry-run]"))
	}
	name, params, dry := args[0], map[string]string{}, false
	for _, a := range args[1:] {
		if a == "--dry-run" || a == "-n" {
			dry = true
			continue
		}
		k, v, ok := strings.Cut(a, "=")
		if !ok {
			die(fmt.Errorf("parameter %q: expected key=value", a))
		}
		params[k] = v
	}
	body := map[string]any{"target": target, "params": params, "reason": reason, "wait": wait, "dry_run": dry}
	if dry {
		var c struct {
			Argv      []string
			Target    string
			LevelName string `json:"level_name"`
			Rule      string
			Outcome   string
		}
		if _, err := call("POST", "/api/v1/commands/"+name+"/run", body, &c); err != nil {
			die(err)
		}
		fmt.Printf("command: %s\ntarget: %s\nlevel: %s\nrule: %s\noutcome: %s\n",
			strings.Join(c.Argv, " "), c.Target, c.LevelName, c.Rule, c.Outcome)
		return
	}
	var r request
	if _, err := call("POST", "/api/v1/commands/"+name+"/run", body, &r); err != nil {
		die(err)
	}
	finish(&r)
}

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

// cmdCmd manages custom commands.
func cmdCmd(args []string) {
	sub := "list"
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "list", "ls":
		var out struct{ Commands []customCommand }
		if _, err := call("GET", "/api/v1/commands", nil, &out); err != nil {
			die(err)
		}
		for _, c := range out.Commands {
			state := ""
			if !c.Enabled {
				state = " (disabled)"
			}
			fmt.Printf("%-4d %-24s L%d  %s%s\n", c.ID, c.Name, c.Level, c.Template, state)
			if c.Description != "" {
				fmt.Printf("     %s\n", c.Description)
			}
			for _, p := range c.Params {
				extra := ""
				if p.Pattern != "" {
					extra += " ~ " + p.Pattern
				}
				if p.Default != "" {
					extra += " (default " + p.Default + ")"
				}
				if p.Optional {
					extra += " (optional)"
				}
				fmt.Printf("     %s%s %s\n", p.Name, extra, p.Description)
			}
			if len(c.Targets) > 0 {
				fmt.Printf("     targets: %s\n", strings.Join(c.Targets, ", "))
			}
		}
	case "add":
		fs := flag.NewFlagSet("gk cmd add", flag.ExitOnError)
		file := fs.String("f", "", "read the command as JSON from a file (- for stdin)")
		c := customCommand{Enabled: true, Level: 1}
		fs.StringVar(&c.Name, "name", "", "command name, e.g. restart-service")
		fs.StringVar(&c.Template, "template", "", `argv template, e.g. "systemctl restart {{service}}"`)
		fs.StringVar(&c.Description, "desc", "", "description")
		fs.IntVar(&c.Level, "level", 1, "risk level 0-3")
		var ps, ts multi
		fs.Var(&ps, "param", "parameter as name[=regexp] (repeatable)")
		fs.Var(&ts, "target", "allowed target (repeatable; default all)")
		fs.Parse(args)
		if *file != "" {
			f := os.Stdin
			if *file != "-" {
				var err error
				if f, err = os.Open(*file); err != nil {
					die(err)
				}
			}
			if err := json.NewDecoder(f).Decode(&c); err != nil {
				die(err)
			}
		} else {
			for _, p := range ps {
				n, re, _ := strings.Cut(p, "=")
				c.Params = append(c.Params, param{Name: n, Pattern: re})
			}
			c.Targets = ts
		}
		path, method := "/api/v1/commands", "POST"
		if c.ID != 0 {
			path, method = fmt.Sprintf("/api/v1/commands/%d", c.ID), "PUT"
		}
		var out customCommand
		if _, err := call(method, path, c, &out); err != nil {
			die(err)
		}
		fmt.Printf("saved command %d %s (L%d)\n", out.ID, out.Name, out.Level)
	case "rm", "delete":
		if len(args) != 1 {
			die(fmt.Errorf("usage: gk cmd rm <id>"))
		}
		if _, err := call("DELETE", "/api/v1/commands/"+args[0], nil, nil); err != nil {
			die(err)
		}
		fmt.Println("deleted")
	default:
		die(fmt.Errorf("usage: gk cmd [list] | add --name N --template T [--param p[=re]]... [--level L] [--target T]... | add -f cmd.json | rm <id>"))
	}
}
