// Command gk is the gatekeep client.
//
//	gk [-t target] [-r reason] [--wait N] <command...>   run a command through gatekeep
//	gk check <command...>                                classify without running
//	gk targets | pending | show <id> | wait <id>
//	gk approve <id> [note] | reject <id> [note] | cancel <id>
//	gk audit [--verify]
//	gk rules [list] | rules add [flags] | rules rm <id>   custom command levels (admin)
//	gk run <name> [key=value...] [--dry-run]            run a custom command
//	gk cmd [list] | cmd add [flags] | cmd rm <id>        custom commands (admin)
//
// Environment: GATEKEEP_URL (default http://127.0.0.1:8740), GATEKEEP_TOKEN,
// GATEKEEP_TARGET (default target).
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

var (
	baseURL = envOr("GATEKEEP_URL", "http://127.0.0.1:8740")
	token   = os.Getenv("GATEKEEP_TOKEN")
	client  = &http.Client{Timeout: 11 * time.Minute}
)

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

type request struct {
	ID           string   `json:"id"`
	CreatedAt    int64    `json:"created_at"`
	Requester    string   `json:"requester"`
	Target       string   `json:"target"`
	Argv         []string `json:"argv"`
	Reason       string   `json:"reason"`
	Level        int      `json:"level"`
	Rule         string   `json:"rule"`
	Status       string   `json:"status"`
	Approver     string   `json:"approver"`
	DecisionNote string   `json:"decision_note"`
	ExitCode     int      `json:"exit_code"`
	Stdout       string   `json:"stdout"`
	Stderr       string   `json:"stderr"`
	Truncated    bool     `json:"truncated"`
	Command      string   `json:"command"`
}

func call(method, path string, body any, out any) (int, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, strings.TrimRight(baseURL, "/")+path, rd)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var e struct{ Error string }
	json.Unmarshal(b, &e)
	if e.Error != "" {
		return resp.StatusCode, fmt.Errorf("%s", e.Error)
	}
	if resp.StatusCode >= 400 && resp.StatusCode != 403 { // 403 carries a denied request
		return resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, b)
	}
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			return resp.StatusCode, err
		}
	}
	return resp.StatusCode, nil
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "gk:", err)
	os.Exit(2)
}

// finish prints a request result and exits with the command's exit code, so
// gk behaves like the wrapped command in scripts.
func finish(r *request) {
	switch r.Status {
	case "succeeded", "failed":
		os.Stdout.WriteString(r.Stdout)
		os.Stderr.WriteString(r.Stderr)
		if r.Truncated {
			fmt.Fprintln(os.Stderr, "gk: output truncated")
		}
		os.Exit(r.ExitCode)
	case "pending":
		fmt.Fprintf(os.Stderr, "gk: %s is L%d (%s) and needs approval: request %s pending\n"+
			"gk: run `gk wait %s` to block until decided\n", strings.Join(r.Argv, " "), r.Level, r.Rule, r.ID, r.ID)
		os.Exit(75) // EX_TEMPFAIL
	case "rejected":
		fmt.Fprintf(os.Stderr, "gk: request %s rejected by %s: %s\n", r.ID, r.Approver, r.DecisionNote)
		os.Exit(77) // EX_NOPERM
	case "denied":
		fmt.Fprintf(os.Stderr, "gk: request %s denied by policy (L%d, rule %s)\n", r.ID, r.Level, r.Rule)
		os.Exit(77)
	default:
		fmt.Fprintf(os.Stderr, "gk: request %s is %s\n", r.ID, r.Status)
		os.Exit(1)
	}
}

func printList(list []request) {
	for _, r := range list {
		fmt.Printf("%s  %-9s L%d  %-10s %-12s %s\n", r.ID, r.Status, r.Level, r.Requester, r.Target, strings.Join(r.Argv, " "))
		if r.Command != "" {
			fmt.Printf("    custom command: %s\n", r.Command)
		}
		if r.Reason != "" {
			fmt.Printf("    reason: %s\n", r.Reason)
		}
	}
}

func main() {
	fs := flag.NewFlagSet("gk", flag.ExitOnError)
	target := fs.String("t", os.Getenv("GATEKEEP_TARGET"), "target")
	reason := fs.String("r", "", "reason (required for changes)")
	wait := fs.Int("wait", 0, "seconds to wait for approval")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: gk [-t target] [-r reason] [--wait N] <command...>\n"+
			"       gk run <custom-command> [key=value...] [--dry-run]\n"+
			"       gk check|targets|pending|show|wait|approve|reject|cancel|audit|rules|cmd ...")
		fs.PrintDefaults()
	}
	fs.Parse(os.Args[1:])
	args := fs.Args()
	if len(args) == 0 {
		fs.Usage()
		os.Exit(2)
	}
	if token == "" {
		die(fmt.Errorf("GATEKEEP_TOKEN is not set"))
	}
	arg := func(i int) string {
		if i < len(args) {
			return args[i]
		}
		return ""
	}
	switch args[0] {
	case "check":
		var c struct {
			LevelName string `json:"level_name"`
			Rule      string
			Outcome   string
		}
		if _, err := call("POST", "/api/v1/requests", map[string]any{"argv": args[1:], "target": *target, "dry_run": true}, &c); err != nil {
			die(err)
		}
		fmt.Printf("level: %s\nrule: %s\noutcome: %s\n", c.LevelName, c.Rule, c.Outcome)
	case "targets":
		var ts []map[string]any
		if _, err := call("GET", "/api/v1/targets", nil, &ts); err != nil {
			die(err)
		}
		for _, t := range ts {
			fmt.Printf("%-16v %-6v %v\n", t["name"], t["transport"], t["host"])
		}
	case "pending":
		var list []request
		if _, err := call("GET", "/api/v1/requests?status=pending", nil, &list); err != nil {
			die(err)
		}
		printList(list)
	case "history":
		var list []request
		if _, err := call("GET", "/api/v1/requests?limit=30", nil, &list); err != nil {
			die(err)
		}
		printList(list)
	case "show", "wait":
		q := ""
		if args[0] == "wait" {
			q = "?wait=600"
		}
		var r request
		if _, err := call("GET", "/api/v1/requests/"+arg(1)+q, nil, &r); err != nil {
			die(err)
		}
		finish(&r)
	case "approve", "reject", "cancel":
		var r request
		if _, err := call("POST", "/api/v1/requests/"+arg(1)+"/"+args[0], map[string]string{"note": strings.Join(args[min(2, len(args)):], " ")}, &r); err != nil {
			die(err)
		}
		fmt.Printf("%s -> %s\n", r.ID, r.Status)
	case "audit":
		if arg(1) == "--verify" {
			var v map[string]any
			if _, err := call("GET", "/api/v1/audit/verify", nil, &v); err != nil {
				die(err)
			}
			fmt.Printf("%v\n", v)
			if v["ok"] != true {
				os.Exit(1)
			}
			return
		}
		var ev []map[string]any
		if _, err := call("GET", "/api/v1/audit?limit=50", nil, &ev); err != nil {
			die(err)
		}
		for _, e := range ev {
			ts := time.UnixMilli(int64(e["ts"].(float64))).Format(time.DateTime)
			fmt.Printf("%6v %s %-10v %-24v %v %v\n", e["seq"], ts, e["actor"], e["action"], e["request_id"], e["detail"])
		}
	case "rules":
		rulesCmd(args[1:])
	case "run":
		runCustom(args[1:], *target, *reason, *wait)
	case "cmd", "cmds":
		cmdCmd(args[1:])
	case "--":
		args = args[1:]
		fallthrough
	default:
		var r request
		if _, err := call("POST", "/api/v1/requests", map[string]any{
			"target": *target, "argv": args, "reason": *reason, "wait": *wait,
		}, &r); err != nil {
			die(err)
		}
		finish(&r)
	}
}

type customRule struct {
	ID        int64  `json:"id,omitempty"`
	Name      string `json:"name"`
	Program   string `json:"program"`
	Args      string `json:"args,omitempty"`
	NotArgs   string `json:"not_args,omitempty"`
	Level     int    `json:"level"`
	Target    string `json:"target,omitempty"`
	Priority  int    `json:"priority"`
	Enabled   bool   `json:"enabled"`
	Note      string `json:"note,omitempty"`
	UpdatedBy string `json:"updated_by,omitempty"`
}

// rulesCmd manages custom (runtime) command levels.
func rulesCmd(args []string) {
	sub := "list"
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "list", "ls":
		var out struct{ Custom []customRule }
		if _, err := call("GET", "/api/v1/rules", nil, &out); err != nil {
			die(err)
		}
		for _, r := range out.Custom {
			state := "on "
			if !r.Enabled {
				state = "off"
			}
			tgt := r.Target
			if tgt == "" {
				tgt = "*"
			}
			fmt.Printf("%4d %s L%d p%-4d %-24s %-10s %s %s", r.ID, state, r.Level, r.Priority, r.Name, tgt, r.Program, r.Args)
			if r.NotArgs != "" {
				fmt.Printf("  !~ %s", r.NotArgs)
			}
			fmt.Println()
		}
	case "add":
		fs := flag.NewFlagSet("rules add", flag.ExitOnError)
		var r customRule
		fs.StringVar(&r.Name, "name", "", "rule name (unique)")
		fs.StringVar(&r.Program, "program", "", "program glob, e.g. kubectl or '{free,df}'")
		fs.StringVar(&r.Args, "args", "", "regexp the argument string must match")
		fs.StringVar(&r.NotArgs, "not-args", "", "regexp the argument string must NOT match")
		fs.IntVar(&r.Level, "level", 2, "0 read-only, 1 low, 2 high, 3 critical")
		fs.StringVar(&r.Target, "target", "", "only apply on this target (default all)")
		fs.IntVar(&r.Priority, "priority", 100, "lower is evaluated first")
		fs.StringVar(&r.Note, "note", "", "why this rule exists")
		fs.Parse(args)
		r.Enabled = true
		if _, err := call("POST", "/api/v1/rules", r, &r); err != nil {
			die(err)
		}
		fmt.Printf("rule %d %q created (L%d)\n", r.ID, r.Name, r.Level)
	case "rm", "delete":
		if len(args) == 0 {
			die(fmt.Errorf("usage: gk rules rm <id>"))
		}
		if _, err := call("DELETE", "/api/v1/rules/"+args[0], nil, nil); err != nil {
			die(err)
		}
		fmt.Printf("rule %s deleted\n", args[0])
	default:
		die(fmt.Errorf("usage: gk rules [list] | add --name N --program P [--args RE] [--level L] ... | rm <id>"))
	}
}
