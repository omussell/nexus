// Package server exposes the Nauvis DOI index over HTTP so callers can map a
// DOI to the input file it was recorded in. It wraps a *store.Store and
// answers lookups over HTTP.
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

// request is the JSON body accepted by the lookup endpoint.
type request struct {
	DOI string `json:"doi"`
}

// New builds a Server that looks items up through the provided store. lg may be
// nil, in which case slog.Default() is used.
func New(st *store.Store, lg *slog.Logger) *Server {
	if lg == nil {
		lg = slog.Default()
	}
	return &Server{st: st, lg: lg}
}

// Response is returned by the lookup endpoint on success or 404. A missing DOI
// is normalised into the error field with a 404 status, so callers only need to
// inspect the file on success.
type Response struct {
	DOI   string `json:"doi,omitempty"`
	File  string `json:"file,omitempty"`
	Error string `json:"error,omitempty"`
}

// Handler returns an http.Handler exposing the single /query endpoint, which
// accepts POST /query with a JSON body of the form {"doi": "<DOI>"}. Using a
// request body (rather than a query string) means any DOI character is safe to
// send verbatim.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/query", s.handleQuery)
	return mux
}

// handleQuery accepts a single DOI in the JSON request body and returns the file
// it was recorded in. See Response and writeJSON for the status rules.
func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	dec := json.NewDecoder(r.Body)
	var req request
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, Response{Error: "invalid JSON body"})
		return
	}
	if req.DOI == "" {
		writeJSON(w, http.StatusBadRequest, Response{Error: "missing 'doi' field"})
		return
	}

	item, err := s.st.GetByDOI(r.Context(), req.DOI)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeJSON(w, http.StatusNotFound, Response{DOI: req.DOI, Error: "no such item"})
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, Response{DOI: req.DOI, Error: err.Error()})
		s.lg.Error("lookup failed", "doi", req.DOI, "err", err)
	default:
		writeJSON(w, http.StatusOK, Response{DOI: req.DOI, File: item.File})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
