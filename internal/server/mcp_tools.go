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
		Target      string `json:"target"`
		Command     string `json:"command"`
		Reason      string `json:"reason"`
		ID          string `json:"id"`
		WaitSeconds int    `json:"wait_seconds"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return toolText(true, "invalid arguments: "+err.Error())
		}
	}
	fail := func(err error) map[string]any { return toolText(true, "error: "+err.Error()) }
	ctx := r.Context()
	switch name {
	case "run":
		argv, err := ParseCommand(nil, a.Command)
		if err != nil {
			return fail(err)
		}
		req, err := s.Submit(ctx, p, a.Target, argv, a.Reason, remoteAddr(r))
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
