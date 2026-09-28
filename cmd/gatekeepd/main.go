// Command gatekeepd is the gatekeep access-control server.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/wutz/gatekeep/internal/config"
	"github.com/wutz/gatekeep/internal/executor"
	"github.com/wutz/gatekeep/internal/policy"
	"github.com/wutz/gatekeep/internal/server"
	"github.com/wutz/gatekeep/internal/store"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "gen-token":
			b := make([]byte, 24)
			rand.Read(b)
			tok := "gk_" + hex.EncodeToString(b)
			fmt.Printf("token:        %s\ntoken_sha256: %s\n", tok, config.HashToken(tok))
			return
		case "verify-audit":
			fs := flag.NewFlagSet("verify-audit", flag.ExitOnError)
			db := fs.String("db", "gatekeep.db", "database path")
			fs.Parse(os.Args[2:])
			st, err := store.Open(*db)
			if err != nil {
				log.Fatal(err)
			}
			v, err := st.Verify()
			if err != nil {
				log.Fatal(err)
			}
			fmt.Printf("%+v\n", v)
			if !v.OK {
				os.Exit(1)
			}
			return
		}
	}
	cfgPath := flag.String("config", "configs/gatekeep.yaml", "config file")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	pol, err := policy.Load(cfg.Policy)
	if err != nil {
		log.Fatalf("policy: %v", err)
	}
	st, err := store.Open(cfg.DB)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer st.Close()

	svc := &server.Service{
		Cfg: cfg, Policy: pol, Store: st, Events: server.NewHub(),
		Exec: &executor.Executor{Timeout: time.Duration(cfg.ExecTimeout) * time.Second, MaxOutput: cfg.MaxOutput},
	}

	var web http.Handler
	if cfg.WebDir != "" {
		web = spa(cfg.WebDir)
	}
	srv := &http.Server{Addr: cfg.Listen, Handler: logRequests(svc.Handler(web)), ReadHeaderTimeout: 10 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		t := time.NewTicker(30 * time.Second)
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if n, _ := st.ExpirePending(time.Now()); n > 0 {
					svc.Events.Publish("request", "")
				}
			}
		}
	}()
	st.Audit("gatekeep", "system", "server.start", "", "", "", map[string]any{"listen": cfg.Listen, "version": server.Version})
	go func() {
		log.Printf("gatekeepd %s listening on %s", server.Version, cfg.Listen)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	<-ctx.Done()
	st.Audit("gatekeep", "system", "server.stop", "", "", "", nil)
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(sctx)
}

// spa serves a built single-page app, falling back to index.html.
func spa(dir string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := filepath.Join(dir, filepath.Clean("/"+r.URL.Path))
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			if !strings.HasPrefix(r.URL.Path, "/assets/") {
				http.ServeFile(w, r, filepath.Join(dir, "index.html"))
				return
			}
		}
		fs.ServeHTTP(w, r)
	})
}

func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		h.ServeHTTP(w, r)
		if r.URL.Path != "/api/v1/events" {
			log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
		}
	})
}
