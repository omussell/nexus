package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/nexus/aquilo/internal/store"
)

// Server is the Aquilo HTTP service that serves nauvis records from a
// vulcanus DuckDB database and relationship triples from nexus SQLite.
//
// Endpoints:
//
//	GET /croid/{croid}?full=true -> nauvis record + relationship triples (200) or 404
//	GET /croid/{croid}            -> nauvis record only (200) or 404
//	GET /relationships/{croid}    -> relationship triples (200) or 404
//	GET /healthz                  -> liveness probe
type Server struct {
	store *store.Store
	addr  string
	srv   *http.Server
}

// New creates a new HTTP server listening at the given address, backed by
// the given data sources.
func New(addr string, s *store.Store) *Server {
	mux := http.NewServeMux()
	srv := &Server{store: s, addr: addr}

	mux.HandleFunc("GET /croid/{croid}", srv.handleCroid)
	mux.HandleFunc("GET /relationships/{croid}", srv.handleRelationships)
	mux.HandleFunc("/healthz", srv.handleHealth)

	srv.srv = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return srv
}

// Run starts the HTTP server and blocks until a signal is received.
func (srv *Server) Run(ctx context.Context) error {
	log.Printf("aquilo: listening on %s", srv.addr)

	errChan := make(chan error, 1)
	go func() {
		if err := srv.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
		close(errChan)
	}()

	select {
	case <-ctx.Done():
	case err := <-errChan:
		return err
	}

	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.srv.Shutdown(shutCtx)
}

// Shutdown gracefully stops the server.
func (srv *Server) Shutdown(ctx context.Context) error {
	return srv.srv.Shutdown(ctx)
}

// WaitSignal starts the server and shuts down on SIGINT/SIGTERM.
func (srv *Server) WaitSignal(ctx context.Context) error {
	sigCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errChan := make(chan error, 1)
	go func() {
		errChan <- srv.Run(sigCtx)
	}()

	select {
	case <-sigCtx.Done():
		log.Println("aquilo: shutting down")
		return srv.Shutdown(context.Background())
	case err := <-errChan:
		return err
	}
}

// --- Handlers ---

func (srv *Server) handleCroid(w http.ResponseWriter, r *http.Request) {
	croid := r.PathValue("croid")
	if croid == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "missing croid path parameter"})
		return
	}

	rec, err := srv.store.NauvisByCroid(r.Context(), croid)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, errorBody{Error: "croid not found"})
		return
	}
	if err != nil {
		log.Printf("aquilo: GET /croid/%s: %v", croid, err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Error: "internal error"})
		return
	}

	resp := map[string]any{
		"croid":             rec.Croid,
		"doi":               rec.DOI,
		"record":            rec.Record,
		"funder_names":      rec.FunderNames,
		"affiliation_names": rec.AffiliationNames,
	}

	// When ?full=true, also inject nexus relationship triples.
	if r.URL.Query().Get("full") == "true" {
		relationships, err := srv.store.RelationshipsByCroid(r.Context(), croid)
		if err != nil {
			log.Printf("aquilo: GET /croid/%s: relationships: %v", croid, err)
			// Non-fatal: return nauvis data without relationships.
		} else {
			if relationships == nil {
				relationships = []store.Relationship{}
			}
			resp["relationships"] = relationships
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (srv *Server) handleRelationships(w http.ResponseWriter, r *http.Request) {
	croid := r.PathValue("croid")
	if croid == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "missing croid path parameter"})
		return
	}

	entries, err := srv.store.RelationshipsByCroid(r.Context(), croid)
	if err != nil {
		log.Printf("aquilo: GET /relationships/%s: %v", croid, err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Error: "internal error"})
		return
	}

	if entries == nil {
		entries = []store.Relationship{}
	}
	writeJSON(w, http.StatusOK, entries)
}

func (srv *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	nauvisCount, err := srv.store.NauvisCount(ctx)
	var nauvisStr string
	if err != nil {
		nauvisStr = fmt.Sprintf("error: %v", err)
	} else {
		nauvisStr = fmt.Sprintf("%d", nauvisCount)
	}

	relCount, err := srv.store.RelationshipCount(ctx)
	var relStr string
	if err != nil {
		relStr = fmt.Sprintf("error: %v", err)
	} else {
		relStr = fmt.Sprintf("%d", relCount)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":          "ok",
		"nauvis_records":  nauvisStr,
		"relationships":   relStr,
	})
}

// --- Types ---

type errorBody struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		return
	}
}
