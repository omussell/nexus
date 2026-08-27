// Package server exposes the Nauvis DOI index over HTTP so callers can map a
// DOI to the input file it was recorded in. It wraps a *store.Store and
// answers DOI lookups over HTTP.
package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/nexus/nauvis/internal/store"
)

// Server wraps a *store.Store and answers DOI lookups over HTTP.
type Server struct {
	st *store.Store
	lg *slog.Logger
}

// New builds a Server that looks items up through the provided store. lg may be
// nil, in which case slog.Default() is used.
func New(st *store.Store, lg *slog.Logger) *Server {
	if lg == nil {
		lg = slog.Default()
	}
	return &Server{st: st, lg: lg}
}

// Handler returns an http.Handler exposing the DOI lookup endpoint.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/query", s.handleQuery)
	return mux
}

// handleQuery answers GET /query?doi=<DOI> with the file that DOI was recorded
// in. The response is {"doi": "<DOI>", "file": "<file>"} on success, or
// {"doi": "<DOI>", "error": "<message>"} with a 404 status when the DOI is not
// recorded. A missing DOI is not an error; database failures are logged.
func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	dois := r.URL.Query()["doi"]
	if len(dois) == 0 {
		http.Error(w, "missing 'doi' query parameter", http.StatusBadRequest)
		return
	}
	if len(dois) > 1 {
		http.Error(w, "expected a single 'doi' query parameter", http.StatusBadRequest)
		return
	}
	doi := dois[0]

	item, err := s.st.GetByDOI(r.Context(), doi)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeJSON(w, http.StatusNotFound, map[string]string{"doi": doi, "error": "no such item"})
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"doi": doi, "error": err.Error()})
		s.lg.Error("lookup failed", "doi", doi, "err", err)
	default:
		writeJSON(w, http.StatusOK, map[string]string{"doi": item.Doi, "file": item.File})
	}
}

// writeJSON writes v as JSON with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
