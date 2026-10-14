// Package server implements the Nexus HTTP service: it serves relationship
// data for a given CROID (both incoming and outgoing) as JSON.
package server

import (
	"encoding/json"
	"net/http"
	"time"

	"database/sql"

	"github.com/nexus/nexus/internal/store"
)

// Server is the Nexus HTTP service.
type Server struct {
	store *store.Store
	logf  func(format string, args ...any)
}

// New builds a Server backed by the given store.
func New(st *store.Store, logf func(format string, args ...any)) *Server {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Server{store: st, logf: logf}
}

// Handler returns the http.Handler exposing:
//
//	GET /croid/{croid} -> all relationships + CROID record (200) or 404
//	GET /healthz       -> liveness probe
//	GET /              -> service banner
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleRoot)
	mux.HandleFunc("GET /croid/{croid}", s.handleCroid)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	return withLogging(mux, s.logf)
}

// -- request / response types ------------------------------------------------

type relEntry struct {
	Croid        string `json:"croid"`
	Relationship string `json:"relationship"`
	RelatedCroid string `json:"related_croid"`
	CreatedAt    string `json:"created_at"`
}

type errorBody struct {
	Error string `json:"error"`
}

// -- handlers ----------------------------------------------------------------

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"service":   "nexus",
		"endpoints": "GET /croid/{croid}",
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleCroid(w http.ResponseWriter, r *http.Request) {
	c := r.PathValue("croid")

	// Fetch relationships where this CROID is the source.
	outgoing, err := s.store.ListBySource(r.Context(), c)
	if err != nil && err != sql.ErrNoRows {
		s.logf("GET /croid/%s: list outgoing: %v", c, err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Error: "internal error"})
		return
	}

	// Fetch relationships where this CROID is the target.
	incoming, err := s.store.ListByTarget(r.Context(), c)
	if err != nil && err != sql.ErrNoRows {
		s.logf("GET /croid/%s: list incoming: %v", c, err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Error: "internal error"})
		return
	}

	// Check if the CROID exists and return 404 if absent.
	_, err = s.store.GetCroidMeta(r.Context(), c)
	if err != nil {
		writeJSON(w, http.StatusNotFound, errorBody{Error: "croid not found"})
		return
	}

	entries := toFlatEntries(c, outgoing, incoming)
	if entries == nil {
		entries = []relEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

// -- helpers -----------------------------------------------------------------

func toFlatEntries(croid string, outgoing, incoming []store.Relationship) []relEntry {
	entries := make([]relEntry, 0, len(outgoing)+len(incoming))
	for _, r := range outgoing {
		entries = append(entries, relEntry{
			Croid:        croid,
			Relationship: r.Relationship,
			RelatedCroid: r.Target,
			CreatedAt:    r.CreatedAt.Format(time.RFC3339),
		})
	}
	for _, r := range incoming {
		entries = append(entries, relEntry{
			Croid:        croid,
			Relationship: r.Relationship,
			RelatedCroid: r.Source,
			CreatedAt:    r.CreatedAt.Format(time.RFC3339),
		})
	}
	return entries
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		http.Error(w, `{"error":"marshal failed"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

func withLogging(next http.Handler, logf func(string, ...any)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if logf != nil {
			logf("%s %s", r.Method, r.URL.Path)
		}
		next.ServeHTTP(w, r)
	})
}
