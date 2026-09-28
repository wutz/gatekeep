package server

import (
	"encoding/json"
	"net/http"
)

// Minimal MCP (Model Context Protocol) server over Streamable HTTP. Each POST
// carries one JSON-RPC message and receives a plain JSON response, which is
// valid per the spec and works with Claude Code, Codex, Cursor, etc.

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

const mcpInstructions = `gatekeep mediates access to production infrastructure (Linux hosts, Kubernetes, Ceph, GPFS).
Use the "run" tool to execute commands. Read-only commands (status, logs, get/describe, ceph -s,
mmlscluster, ...) run immediately. Anything that changes state is queued for human approval:
always pass a clear "reason", then use "wait_request" to block until a human decides. Do not try to
work around the policy (no shell pipes, redirects or ';' — pass one plain command; filter output
yourself). Use "check" first if unsure whether a command is read-only.`

func (s *Service) handleMCP(w http.ResponseWriter, r *http.Request) {
	var req rpcReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, 400, rpcResp{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcErr{-32700, "parse error"}})
		return
	}
	if len(req.ID) == 0 { // notification
		w.WriteHeader(http.StatusAccepted)
		return
	}
	resp := rpcResp{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		json.Unmarshal(req.Params, &params)
		pv := params.ProtocolVersion
		if pv == "" {
			pv = "2025-06-18"
		}
		resp.Result = map[string]any{
			"protocolVersion": pv,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "gatekeep", "version": Version},
			"instructions":    mcpInstructions,
		}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		resp.Result = map[string]any{"tools": mcpTools()}
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			resp.Error = &rpcErr{-32602, "invalid params"}
			break
		}
		resp.Result = s.callTool(r, params.Name, params.Arguments)
	default:
		resp.Error = &rpcErr{-32601, "method not found: " + req.Method}
	}
	writeJSON(w, 200, resp)
}
