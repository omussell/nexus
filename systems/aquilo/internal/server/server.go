// Package server provides an HTTP API for accessing items stored in the
// local SQLite database.
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/nexus/aquilo/internal/db"
	"github.com/nexus/aquilo/internal/store"
)

// Server holds the HTTP server and its dependencies.
type Server struct {
	store *store.Store
	addr  string
	srv   *http.Server
}

// New creates a new HTTP server listening at the given address, backed by
// the given database store.
func New(addr string, s *store.Store) *Server {
	mux := http.NewServeMux()
	srv := &Server{store: s, addr: addr}

	mux.HandleFunc("GET /status", srv.handleStatus)
	mux.HandleFunc("GET /items/count", srv.handleItemsCount)
	mux.HandleFunc("GET /items", srv.handleItemsList)
	mux.HandleFunc("GET /items/{croid}", srv.handleItemByCroid)
	mux.HandleFunc("GET /croid/{croid}", srv.handleItemByCroidJSON)

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

func (srv *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	count, err := srv.store.CountItems(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items":  count,
		"status": "ok",
	})
}

func (srv *Server) handleItemsCount(w http.ResponseWriter, r *http.Request) {
	count, err := srv.store.CountItems(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"count": count,
	})
}

func (srv *Server) handleItemsList(w http.ResponseWriter, r *http.Request) {
	offset := 0
	limit := 50

	if q := r.URL.Query().Get("offset"); q != "" {
		parsed, err := strconv.ParseInt(q, 10, 64)
		if err == nil {
			offset = int(parsed)
		}
	}
	if q := r.URL.Query().Get("limit"); q != "" {
		parsed, err := strconv.ParseInt(q, 10, 64)
		if err == nil {
			limit = int(parsed)
		}
	}

	if limit <= 0 || limit > 500 {
		limit = 50
	}

	items, err := srv.store.QueryItems(r.Context(), offset, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	type itemEnvelope struct {
		Croid  string `json:"croid"`
		Record string `json:"record"`
	}

	envelopes := make([]itemEnvelope, len(items))
	for i, it := range items {
		envelopes[i] = itemEnvelope{
			Croid:  it.Croid,
			Record: string(db.JSONB(it.RecordJson)),
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items":  envelopes,
		"total":  len(envelopes),
		"offset": offset,
		"limit":  limit,
	})
}

func (srv *Server) handleItemByCroid(w http.ResponseWriter, r *http.Request) {
	croid := r.PathValue("croid")
	if croid == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing croid"})
		return
	}

	item, err := srv.store.GetItemByCroid(r.Context(), croid)
	if err != nil {
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "item not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"croid":  item.Croid,
		"record": string(db.JSONB(item.RecordJson)),
	})
}

func (srv *Server) handleItemByCroidJSON(w http.ResponseWriter, r *http.Request) {
	croid := r.PathValue("croid")
	if croid == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing croid"})
		return
	}

	item, err := srv.store.GetItemByCroid(r.Context(), croid)
	if err != nil {
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "item not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(item.RecordJson)); err != nil {
		log.Printf("aquilo: write response: %v", err)
	}
}

// writeJSON sets the Content-Type header and writes the JSON response.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if payload != nil && status != http.StatusNoContent {
		json.NewEncoder(w).Encode(payload)
	}
}
