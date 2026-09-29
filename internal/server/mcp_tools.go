package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/wutz/gatekeep/internal/store"
)

// Version is set at build time.
var Version = "0.1.0"

func obj(props map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": required}
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

func mcpTools() []map[string]any {
	ro := map[string]any{"readOnlyHint": true}
	return []map[string]any{
		{
			"name": "run",
			"description": "Run a command on a production target. Read-only commands return output immediately. " +
				"State-changing commands are queued for human approval and return a pending request id " +
				"(then call wait_request). Examples: 'kubectl get pods -A', 'journalctl -u kubelet -n 200', " +
				"'ceph -s', 'mmgetstate -a', 'df -h'. No pipes/redirects; one command per call.",
			"inputSchema": obj(map[string]any{
				"target":  str("Target name from list_targets. Optional: defaults to the server's default target."),
				"command": str("The command line, e.g. \"kubectl get nodes -o wide\"."),
				"reason":  str("Why you need this. Required for anything that is not read-only; shown to the approver."),
				"wait_seconds": map[string]any{"type": "integer",
					"description": "If approval is needed, block up to this many seconds (max 600) for the decision. Default 0."},
			}, "command"),
		},
		{
			"name":        "check",
			"description": "Dry-run: classify a command's risk level and whether you could run it directly, without executing it.",
			"inputSchema": obj(map[string]any{
				"command": str("The command line to classify."),
				"target":  str("Optional target; rules can differ per target."),
			}, "command"),
			"annotations": ro,
		},
		{
			"name": "list_commands",
			"description": "List the custom commands defined by your admins: named, parameterized operations " +
				"(e.g. restart-service) with their parameters, risk level and allowed targets. " +
				"Prefer these over hand-written commands when one fits.",
			"inputSchema": obj(map[string]any{}),
			"annotations": ro,
		},
		{
			"name": "run_command",
			"description": "Run a custom command (see list_commands) by name with parameter values. " +
				"Same approval flow as run: above your level it is queued for human approval.",
			"inputSchema": obj(map[string]any{
				"name":    str("Custom command name from list_commands."),
				"params":  map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "Parameter values, e.g. {\"service\": \"kubelet\"}."},
				"target":  str("Optional target; must be one the command allows."),
				"reason":  str("Why you need this. Required when approval is needed."),
				"dry_run": map[string]any{"type": "boolean", "description": "Only render and classify; do not run."},
				"wait_seconds": map[string]any{"type": "integer",
					"description": "If approval is needed, block up to this many seconds (max 600). Default 0."},
			}, "name"),
		},
		{
			"name":        "list_targets",
			"description": "List the hosts / clusters you can run commands on.",
			"inputSchema": obj(map[string]any{}),
			"annotations": ro,
		},
		{
			"name":        "get_request",
			"description": "Get status and output of a request you submitted.",
			"inputSchema": obj(map[string]any{"id": str("Request id, e.g. req_1a2b...")}, "id"),
			"annotations": ro,
		},
		{
			"name":        "wait_request",
			"description": "Block until a pending request is approved+executed, rejected or expired (or timeout). Returns the final output.",
			"inputSchema": obj(map[string]any{
				"id":           str("Request id."),
				"wait_seconds": map[string]any{"type": "integer", "description": "Max seconds to wait (default 300, max 600)."},
			}, "id"),
			"annotations": ro,
		},
		{
			"name":        "cancel_request",
			"description": "Withdraw one of your pending requests.",
			"inputSchema": obj(map[string]any{"id": str("Request id.")}, "id"),
		},
	}
}

func toolText(isErr bool, text string) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": isErr}
}

func formatRequest(r *store.Request) string {
	var b strings.Builder
	fmt.Fprintf(&b, "request: %s\nstatus: %s\ntarget: %s\ncommand: %s\nlevel: L%d (rule %s)\n",
		r.ID, r.Status, r.Target, strings.Join(r.Argv, " "), r.Level, r.Rule)
	if r.Command != "" {
		fmt.Fprintf(&b, "custom command: %s\n", r.Command)
	}
	switch r.Status {
	case store.StatusPending:
		fmt.Fprintf(&b, "\nThis command changes state and is waiting for human approval (expires %s).\n"+
			"Call wait_request with id %q to block until it is decided.\n",
			time.UnixMilli(r.ExpiresAt).Format(time.RFC3339), r.ID)
	case store.StatusRejected:
		fmt.Fprintf(&b, "rejected by: %s\nnote: %s\n", r.Approver, r.DecisionNote)
	case store.StatusDenied:
		b.WriteString("\nDenied by policy: your role may not request this operation.\n")
	case store.StatusSucceeded, store.StatusFailed:
		if r.Approver != "" && r.Approver != r.Requester {
			fmt.Fprintf(&b, "approved by: %s\n", r.Approver)
		}
		fmt.Fprintf(&b, "exit_code: %d\n", r.ExitCode)
		if r.Stdout != "" {
			b.WriteString("\n--- stdout ---\n" + r.Stdout)
		}
		if r.Stderr != "" {
			b.WriteString("\n--- stderr ---\n" + r.Stderr)
		}
		if r.Truncated {
			b.WriteString("\n[output truncated; narrow the command, e.g. use -n/--tail/--since]\n")
		}
	}
	return b.String()
}

func (s *Service) callTool(r *http.Request, name string, raw json.RawMessage) map[string]any {
	p := principalFrom(r)
	var a struct {
		Target      string            `json:"target"`
		Command     string            `json:"command"`
		Reason      string            `json:"reason"`
		ID          string            `json:"id"`
		WaitSeconds int               `json:"wait_seconds"`
		Name        string            `json:"name"`
		Params      map[string]string `json:"params"`
		DryRun      bool              `json:"dry_run"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return toolText(true, "invalid arguments: "+err.Error())
		}
	}
	fail := func(err error) map[string]any { return toolText(true, "error: "+err.Error()) }
	ctx := r.Context()
	switch name {
	case "run", "run_command":
		var req *store.Request
		var err error
		if name == "run" {
			var argv []string
			if argv, err = ParseCommand(nil, a.Command); err != nil {
				return fail(err)
			}
			req, err = s.Submit(ctx, p, a.Target, argv, a.Reason, remoteAddr(r))
		} else if a.DryRun {
			c, err := s.CheckCommand(p, a.Name, a.Target, a.Params)
			if err != nil {
				return fail(err)
			}
			return toolText(false, fmt.Sprintf("command: %s\ntarget: %s\nlevel: %s\nrule: %s\noutcome for you: %s",
				strings.Join(c.Argv, " "), c.Target, c.LevelS, c.Rule, c.Outcome))
		} else {
			req, err = s.RunCommand(ctx, p, a.Name, a.Target, a.Params, a.Reason, remoteAddr(r))
		}
		if err != nil {
			return fail(err)
		}
		if req.Status == store.StatusPending && a.WaitSeconds > 0 {
			if req, err = s.Wait(ctx, p, req.ID, time.Duration(min(a.WaitSeconds, 600))*time.Second); err != nil {
				return fail(err)
			}
		}
		return toolText(req.Status == store.StatusFailed || req.Status == store.StatusDenied ||
			req.Status == store.StatusRejected, formatRequest(req))
	case "check":
		argv, err := ParseCommand(nil, a.Command)
		if err != nil {
			return fail(err)
		}
		c, err := s.Check(p, a.Target, argv)
		if err != nil {
			return fail(err)
		}
		return toolText(false, fmt.Sprintf("level: %s\nrule: %s\noutcome for you: %s", c.LevelS, c.Rule, c.Outcome))
	case "list_commands":
		l, err := s.ListCommands(p)
		if err != nil {
			return fail(err)
		}
		if len(l.Commands) == 0 {
			return toolText(false, "No custom commands are defined.")
		}
		var b strings.Builder
		for _, c := range l.Commands {
			fmt.Fprintf(&b, "- %s [%s]: %s\n  template: %s\n", c.Name, c.Level, c.Description, c.Template)
			if len(c.Targets) > 0 {
				fmt.Fprintf(&b, "  targets: %s\n", strings.Join(c.Targets, ", "))
			}
			for _, pr := range c.Params {
				opt := ""
				if pr.Optional {
					opt = ", optional"
				}
				if pr.Default != "" {
					opt += ", default " + pr.Default
				}
				fmt.Fprintf(&b, "  param %s (%s%s): %s\n", pr.Name, pr.EffectivePattern(), opt, pr.Description)
			}
		}
		return toolText(false, b.String())
	case "list_targets":
		var b strings.Builder
		for _, t := range s.Cfg.Targets {
			if p.CanUseTarget(t.Name) {
				def := ""
				if t.Name == s.Cfg.DefaultTarget {
					def = " [default]"
				}
				fmt.Fprintf(&b, "- %s%s (%s) %s %s\n", t.Name, def, strings.Join(t.Labels, ","), t.Host, t.Description)
			}
		}
		return toolText(false, b.String())
	case "get_request":
		req, err := s.Get(p, a.ID)
		if err != nil {
			return fail(err)
		}
		return toolText(false, formatRequest(req))
	case "wait_request":
		if a.WaitSeconds <= 0 {
			a.WaitSeconds = 300
		}
		req, err := s.Wait(ctx, p, a.ID, time.Duration(min(a.WaitSeconds, 600))*time.Second)
		if err != nil {
			return fail(err)
		}
		return toolText(false, formatRequest(req))
	case "cancel_request":
		req, err := s.Cancel(p, a.ID, remoteAddr(r))
		if err != nil {
			return fail(err)
		}
		return toolText(false, formatRequest(req))
	}
	return toolText(true, "unknown tool "+name)
}
