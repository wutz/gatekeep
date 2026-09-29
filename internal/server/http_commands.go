package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/wutz/gatekeep/internal/commands"
)

type runCommandBody struct {
	Target string            `json:"target"`
	Params map[string]string `json:"params"`
	Reason string            `json:"reason"`
	Wait   int               `json:"wait"`
	DryRun bool              `json:"dry_run"`
}

func (s *Service) commandRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/commands", s.withAuth(func(w http.ResponseWriter, r *http.Request) {
		l, err := s.ListCommands(principalFrom(r))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, 200, l)
	}))
	mux.HandleFunc("POST /api/v1/commands", s.withAuth(s.humansOnly(s.handleSaveCommand)))
	mux.HandleFunc("PUT /api/v1/commands/{id}", s.withAuth(s.humansOnly(s.handleSaveCommand)))
	mux.HandleFunc("DELETE /api/v1/commands/{id}", s.withAuth(s.humansOnly(func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err := s.DeleteCommand(principalFrom(r), id, remoteAddr(r)); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})))
	mux.HandleFunc("POST /api/v1/commands/{name}/run", s.withAuth(func(w http.ResponseWriter, r *http.Request) {
		p := principalFrom(r)
		var b runCommandBody
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&b); err != nil {
			writeErr(w, errf(400, "bad json: %v", err))
			return
		}
		name := r.PathValue("name")
		if b.DryRun {
			c, err := s.CheckCommand(p, name, b.Target, b.Params)
			if err != nil {
				writeErr(w, err)
				return
			}
			writeJSON(w, 200, c)
			return
		}
		req, err := s.RunCommand(r.Context(), p, name, b.Target, b.Params, b.Reason, remoteAddr(r))
		s.writeSubmitted(w, r, req, err, b.Wait)
	}))
}

func (s *Service) handleSaveCommand(w http.ResponseWriter, r *http.Request) {
	var c commands.Command
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&c); err != nil {
		writeErr(w, errf(400, "bad json: %v", err))
		return
	}
	c.ID = 0
	if id := r.PathValue("id"); id != "" {
		c.ID, _ = strconv.ParseInt(id, 10, 64)
		if c.ID <= 0 {
			writeErr(w, errf(400, "bad command id"))
			return
		}
	}
	out, err := s.SaveCommand(principalFrom(r), &c, remoteAddr(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, out)
}
