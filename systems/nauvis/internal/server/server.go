// Package server exposes the Nauvis DOI index and records over HTTP so callers
// can look up a DOI's file or fetch the full record JSON. It wraps a
// *store.Store and answers lookups over HTTP.
package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/nexus/nauvis/internal/store"
)

// Server wraps a *store.Store and answers DOI lookups over HTTP.
type Server struct {
	st   *store.Store
	lg   *slog.Logger
	data string // output directory with NDJSON files

	idxMu   sync.RWMutex
	idx     map[string]string // DOI -> filename
	idxErr  error
}

// request is the JSON body accepted by the lookup endpoint.
type request struct {
	DOI string `json:"doi"`
}

// New builds a Server that looks items up through the provided store. lg may be
// nil, in which case slog.Default() is used. data is the output directory
// containing NDJSON files; it is used to serve full records on demand.
func New(st *store.Store, lg *slog.Logger, data string) *Server {
	if lg == nil {
		lg = slog.Default()
	}
	return &Server{st: st, lg: lg, data: data}
}

// Response is returned by the lookup endpoint on success or 404. A missing DOI
// is normalised into the error field with a 404 status, so callers only need to
// inspect the file on success.
type Response struct {
	DOI   string `json:"doi,omitempty"`
	File  string `json:"file,omitempty"`
	Error string `json:"error,omitempty"`
}

// Handler returns an http.Handler exposing the following endpoints:
//
//	POST /query   - look up a DOI's file ({"doi": "..."})
//	GET  /record?doi=X - serve the full record JSON for a DOI
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/query", s.handleQuery)
	mux.HandleFunc("GET /record", s.handleRecord)
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

// handleRecord serves the full record JSON for the given DOI. The record is
// read from the NDJSON output file that contains it.
func (s *Server) handleRecord(w http.ResponseWriter, r *http.Request) {
	doi := r.URL.Query().Get("doi")
	if doi == "" {
		writeJSON(w, http.StatusBadRequest, Response{Error: "missing doi"})
		return
	}

	// Load index on first request (on-demand).
	if err := s.loadIndex(); err != nil {
		s.lg.Error("record: load index", "err", err)
		writeJSON(w, http.StatusInternalServerError, Response{Error: "internal error"})
		return
	}

	record, err := s.LoadRecord(doi)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, Response{DOI: doi, Error: "no such DOI"})
		return
	}
	if err != nil {
		s.lg.Error("record: load", "doi", doi, "err", err)
		writeJSON(w, http.StatusInternalServerError, Response{DOI: doi, Error: "failed to read record"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(record)
}

// loadIndex builds the on-demand DOI-to-file index by scanning all NDJSON files
// in the output directory. Subsequent calls are no-ops.
func (s *Server) loadIndex() error {
	s.idxMu.Lock()
	defer s.idxMu.Unlock()
	if s.idx != nil {
		return nil
	}

	s.idx = make(map[string]string)
	pattern := filepath.Join(filepath.Clean(s.data), "*.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		s.idxErr = err
		return err
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		// Each line is a JSON value; extract the DOI.
		for _, line := range bytes.Split(data, []byte("\n")) {
			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(line, &raw); err != nil {
				continue
			}
			if doiBytes, ok := raw["DOI"]; ok {
				var doi string
				if err := json.Unmarshal(doiBytes, &doi); err == nil && doi != "" {
					s.idx[doi] = filepath.Base(f)
				}
			}
		}
	}
	return nil
}

// LoadRecord returns the full record JSON for the given DOI by reading the
// corresponding NDJSON file.
func (s *Server) LoadRecord(doi string) ([]byte, error) {
	s.idxMu.RLock()
	file, ok := s.idx[doi]
	s.idxMu.RUnlock()
	if !ok {
		return nil, sql.ErrNoRows
	}
	return s.loadRecordByDOI(file, doi)
}

func (s *Server) loadRecordByDOI(filename, doi string) ([]byte, error) {
	path := filepath.Join(filepath.Clean(s.data), filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(line, &raw); err != nil {
			continue
		}
		if rawDOIBytes, ok := raw["DOI"]; ok {
			var rawDOI string
			if err := json.Unmarshal(rawDOIBytes, &rawDOI); err == nil && rawDOI == doi {
				return line, nil
			}
		}
	}
	return nil, errors.New("record not found in file")
}
