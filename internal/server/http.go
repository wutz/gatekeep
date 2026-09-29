package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/wutz/gatekeep/internal/config"
	"github.com/wutz/gatekeep/internal/policy"
	"github.com/wutz/gatekeep/internal/store"
)

type ctxKey struct{}

func principalFrom(r *http.Request) *config.Principal {
	p, _ := r.Context().Value(ctxKey{}).(*config.Principal)
	return p
}

func remoteAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// authenticate resolves the bearer token (header, or ?token= for SSE).
func (s *Service) authenticate(r *http.Request) *config.Principal {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if tok == "" || tok == r.Header.Get("Authorization") {
		tok = r.URL.Query().Get("token")
	}
	if tok == "" {
		return nil
	}
	h := config.HashToken(tok)
	for i := range s.Cfg.Principals {
		p := &s.Cfg.Principals[i]
		if subtle.ConstantTimeCompare([]byte(h), []byte(p.TokenSHA256)) == 1 {
			return p
		}
	}
	return nil
}

func (s *Service) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := s.authenticate(r)
		if p == nil {
			s.Store.Audit("anonymous", "unknown", "auth.fail", "", "", remoteAddr(r), map[string]string{"path": r.URL.Path})
			writeErr(w, errf(401, "missing or invalid token"))
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, p)))
	}
}

func (s *Service) humansOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if p := principalFrom(r); p.Kind != config.Human {
			s.Store.Audit(p.Name, string(p.Kind), "access.forbidden", r.PathValue("id"), "", remoteAddr(r),
				map[string]string{"method": r.Method, "path": r.URL.Path})
			writeErr(w, errf(403, "this endpoint is for humans only"))
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	var e *Error
	if errors.As(err, &e) {
		writeJSON(w, e.Code, map[string]string{"error": e.Msg})
		return
	}
	writeJSON(w, 500, map[string]string{"error": err.Error()})
}

type submitBody struct {
	Target  string   `json:"target"`
	Argv    []string `json:"argv"`
	Command string   `json:"command"`
	Reason  string   `json:"reason"`
	DryRun  bool     `json:"dry_run"`
	// Wait blocks up to N seconds for a pending request to be decided and run.
	Wait int `json:"wait"`
}

// Handler returns the HTTP handler for the API, MCP endpoint and web UI.
func (s *Service) Handler(web http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /api/v1/me", s.withAuth(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, principalFrom(r))
	}))
	mux.HandleFunc("GET /api/v1/targets", s.withAuth(func(w http.ResponseWriter, r *http.Request) {
		p := principalFrom(r)
		out := []config.Target{}
		for _, t := range s.Cfg.Targets {
			if p.CanUseTarget(t.Name) {
				out = append(out, t)
			}
		}
		writeJSON(w, 200, out)
	}))
	mux.HandleFunc("GET /api/v1/policy", s.withAuth(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, s.Policy)
	}))
	mux.HandleFunc("GET /api/v1/rules", s.withAuth(s.humansOnly(func(w http.ResponseWriter, r *http.Request) {
		custom, err := s.Store.ListRules()
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"custom": custom, "builtin": s.Policy.Rules,
			"default_level": s.Policy.DefaultLevel, "can_edit": canManageRules(principalFrom(r))})
	})))
	mux.HandleFunc("POST /api/v1/rules", s.withAuth(s.humansOnly(s.handleSaveRule)))
	mux.HandleFunc("PUT /api/v1/rules/{id}", s.withAuth(s.humansOnly(s.handleSaveRule)))
	mux.HandleFunc("DELETE /api/v1/rules/{id}", s.withAuth(s.humansOnly(func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err := s.DeleteRule(principalFrom(r), id, remoteAddr(r)); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})))
	mux.HandleFunc("POST /api/v1/rules/test", s.withAuth(s.humansOnly(func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Rule     policy.CustomRule `json:"rule"`
			Target   string            `json:"target"`
			Commands []string          `json:"commands"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&b); err != nil {
			writeErr(w, errf(400, "bad json: %v", err))
			return
		}
		res, err := s.TestRule(principalFrom(r), b.Rule, b.Target, b.Commands)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, 200, res)
	})))
	s.commandRoutes(mux)
	mux.HandleFunc("POST /api/v1/requests", s.withAuth(s.handleSubmit))
	mux.HandleFunc("GET /api/v1/requests", s.withAuth(func(w http.ResponseWriter, r *http.Request) {
		p := principalFrom(r)
		f := store.RequestFilter{Status: r.URL.Query().Get("status"), Requester: r.URL.Query().Get("requester")}
		f.Limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
		if p.Kind == config.Agent {
			f.Requester = p.Name
		}
		list, err := s.Store.ListRequests(f)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, 200, list)
	}))
	mux.HandleFunc("GET /api/v1/requests/{id}", s.withAuth(func(w http.ResponseWriter, r *http.Request) {
		p := principalFrom(r)
		var req *store.Request
		var err error
		if wait, _ := strconv.Atoi(r.URL.Query().Get("wait")); wait > 0 {
			req, err = s.Wait(r.Context(), p, r.PathValue("id"), time.Duration(min(wait, 600))*time.Second)
		} else {
			req, err = s.Get(p, r.PathValue("id"))
		}
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, 200, req)
	}))
	mux.HandleFunc("POST /api/v1/requests/{id}/approve", s.withAuth(s.humansOnly(s.handleDecide(true))))
	mux.HandleFunc("POST /api/v1/requests/{id}/reject", s.withAuth(s.humansOnly(s.handleDecide(false))))
	mux.HandleFunc("POST /api/v1/requests/{id}/cancel", s.withAuth(func(w http.ResponseWriter, r *http.Request) {
		req, err := s.Cancel(principalFrom(r), r.PathValue("id"), remoteAddr(r))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, 200, req)
	}))
	mux.HandleFunc("GET /api/v1/audit", s.withAuth(s.humansOnly(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		f := store.AuditFilter{Actor: q.Get("actor"), Action: q.Get("action"), RequestID: q.Get("request_id")}
		f.Before, _ = strconv.ParseInt(q.Get("before"), 10, 64)
		f.Limit, _ = strconv.Atoi(q.Get("limit"))
		list, err := s.Store.ListAudit(f)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, 200, list)
	})))
	mux.HandleFunc("GET /api/v1/audit/verify", s.withAuth(s.humansOnly(func(w http.ResponseWriter, r *http.Request) {
		v, err := s.Store.Verify()
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, 200, v)
	})))
	mux.HandleFunc("GET /api/v1/events", s.withAuth(s.humansOnly(s.handleEvents)))
	mux.HandleFunc("POST /mcp", s.withAuth(s.handleMCP))
	mux.HandleFunc("GET /mcp", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed) // no server-initiated stream
	})
	if web != nil {
		mux.Handle("/", web)
	}
	return mux
}

func (s *Service) handleSubmit(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	var b submitBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&b); err != nil {
		writeErr(w, errf(400, "bad json: %v", err))
		return
	}
	argv, err := ParseCommand(b.Argv, b.Command)
	if err != nil {
		writeErr(w, err)
		return
	}
	if b.DryRun {
		c, err := s.Check(p, b.Target, argv)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, 200, c)
		return
	}
	req, err := s.Submit(r.Context(), p, b.Target, argv, b.Reason, remoteAddr(r))
	s.writeSubmitted(w, r, req, err, b.Wait)
}

// writeSubmitted optionally waits for a pending request, then replies with
// 200 (done), 202 (pending) or 403 (denied by policy).
func (s *Service) writeSubmitted(w http.ResponseWriter, r *http.Request, req *store.Request, err error, wait int) {
	p := principalFrom(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if req.Status == store.StatusPending && wait > 0 {
		req, err = s.Wait(r.Context(), p, req.ID, time.Duration(min(wait, 600))*time.Second)
		if err != nil {
			writeErr(w, err)
			return
		}
	}
	code := 200
	if req.Status == store.StatusPending {
		code = 202
	} else if req.Status == store.StatusDenied {
		code = 403
	}
	writeJSON(w, code, req)
}

func (s *Service) handleSaveRule(w http.ResponseWriter, r *http.Request) {
	var rule policy.CustomRule
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&rule); err != nil {
		writeErr(w, errf(400, "bad json: %v", err))
		return
	}
	rule.ID = 0
	if id := r.PathValue("id"); id != "" {
		rule.ID, _ = strconv.ParseInt(id, 10, 64)
		if rule.ID <= 0 {
			writeErr(w, errf(400, "bad rule id"))
			return
		}
	}
	out, err := s.SaveRule(principalFrom(r), &rule, remoteAddr(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, out)
}

func (s *Service) handleDecide(approve bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Note string `json:"note"`
		}
		json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&b)
		req, err := s.Decide(r.Context(), principalFrom(r), r.PathValue("id"), approve, b.Note, remoteAddr(r))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, 200, req)
	}
}

func (s *Service) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, errf(500, "streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	sub := s.Events.Subscribe()
	defer s.Events.Unsubscribe(sub)
	w.Write([]byte(": hello\n\n"))
	fl.Flush()
	tick := time.NewTicker(25 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case b := <-sub:
			w.Write([]byte("data: "))
			w.Write(b)
			w.Write([]byte("\n\n"))
			fl.Flush()
		case <-tick.C:
			w.Write([]byte(": ping\n\n"))
			fl.Flush()
		}
	}
}
