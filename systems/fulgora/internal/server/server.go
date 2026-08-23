// Package server implements the Fulgora HTTP service: it exposes which
// dataset versions are available (and where) to other systems, backed by the
// SQLite registry.
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/nexus/fulgora/internal/db"
	"github.com/nexus/fulgora/internal/migrate"
	"github.com/nexus/fulgora/internal/store"

	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver (pure Go)
)

// Server is the Fulgora HTTP service.
type Server struct {
	db    *sql.DB
	store *store.Store
	logf  func(format string, args ...any)
}

// New opens the SQLite database at dbPath, applies the schema, and returns a
// ready Server. Call Close to release the underlying connection.
func New(ctx context.Context, dbPath string, logf func(format string, args ...any)) (*Server, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if strings.TrimSpace(dbPath) == "" {
		return nil, errors.New("server: empty database path")
	}

	// _fk enables foreign keys; _busy_timeout waits up to 5s for the write lock.
	dsn := "file:" + dbPath + "?_fk=1&_busy_timeout=5000"
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite is a single-writer store: cap the pool to one writer to keep
	// concurrent goroutines serialized.
	conn.SetMaxOpenConns(1)

	if err := conn.PingContext(ctx); err != nil {
		conn.Close()
		return nil, err
	}
	if _, err := conn.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		conn.Close()
		return nil, err
	}
	if err := migrate.Apply(ctx, conn); err != nil {
		conn.Close()
		return nil, err
	}

	return &Server{db: conn, store: store.New(db.New(conn)), logf: logf}, nil
}

// Close releases the SQLite connection.
func (s *Server) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// Handler returns the http.Handler exposing:
//
//	GET  /                       -> service banner
//	GET  /healthz                -> liveness probe
//	GET  /latest                 -> every source's latest version
//	GET  /latest/{source}        -> one source's latest version
//	GET  /latest/{source}/history -> every version of a source
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleRoot)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /latest", s.handleAllLatest)
	mux.HandleFunc("GET /latest/{source}", s.handleLatest)
	mux.HandleFunc("GET /latest/{source}/history", s.handleHistory)
	return withLogging(mux, s.logf)
}

type datasetView struct {
	Source       string `json:"source"`
	Version      string `json:"version"`
	CollectedAt  string `json:"collected_at"`
	InitialInput string `json:"initial_input"`
	Output       string `json:"output"`
}

type errorBody struct {
	Error string `json:"error"`
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"service":   "fulgora",
		"endpoints": "GET /latest, GET /latest/{source}, GET /latest/{source}/history, GET /healthz",
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.db.PingContext(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, errorBody{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAllLatest(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListLatest(r.Context())
	if err != nil {
		s.logf("GET /latest: %v", err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Error: "internal error"})
		return
	}
	views := make([]datasetView, len(rows))
	for i, row := range rows {
		views[i] = toView(row)
	}
	writeJSON(w, http.StatusOK, views)
}

func (s *Server) handleLatest(w http.ResponseWriter, r *http.Request) {
	src := r.PathValue("source")
	row, err := s.store.Latest(r.Context(), src)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, errorBody{Error: "source not found: " + src})
		return
	}
	if err != nil {
		s.logf("GET /latest/%s: %v", src, err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Error: "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, toView(row))
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	src := r.PathValue("source")
	rows, err := s.store.ListBySource(r.Context(), src)
	if err != nil {
		s.logf("GET /latest/%s/history: %v", src, err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Error: "internal error"})
		return
	}
	views := make([]datasetView, len(rows))
	for i, row := range rows {
		views[i] = datasetView{
			Source:       row.Source,
			Version:      row.Version,
			CollectedAt:  row.CollectedAt.Format(timeRFC3339),
			InitialInput: row.InitialInput,
			Output:       row.Output,
		}
	}
	writeJSON(w, http.StatusOK, views)
}

func toView(row store.LatestInfo) datasetView {
	return datasetView{
		Source:       row.Source,
		Version:      row.Version,
		CollectedAt:  row.CollectedAt,
		InitialInput: row.InitialInput,
		Output:       row.Output,
	}
}

const timeRFC3339 = "2006-01-02T15:04:05Z07:00"

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		return
	}
}

func withLogging(next http.Handler, logf func(string, ...any)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if logf != nil {
			logf("%s %s", r.Method, r.URL.Path)
		}
		next.ServeHTTP(w, r)
	})
}
