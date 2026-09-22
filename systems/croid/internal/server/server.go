// Package server implements the CROID HTTP service: it mints CROIDs via POST
// and serves them back by GET, backed by a SQLite database.
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/nexus/croid/internal/db"
	"github.com/nexus/croid/internal/migrate"
	"github.com/nexus/croid/internal/rabbitmq"
	"github.com/nexus/croid/internal/store"

	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver (pure Go)
)

// Server is the CROID HTTP service.
type Server struct {
	db      *sql.DB
	store   *store.Store
	logf    func(format string, args ...any)
	publisher *rabbitmq.Publisher
}

// New opens the SQLite database at dbPath, applies the schema, and returns a
// ready Server. Call Close to release the underlying connection.
func New(ctx context.Context, dbPath string, logf func(format string, args ...any)) (*Server, error) {
	return NewWithAMQP(ctx, dbPath, "", "", logf)
}

// NewWithAMQP opens the SQLite database at dbPath, applies the schema,
// connects to the RabbitMQ server at amqpURL, declares the named exchange,
// and returns a ready Server. Call Close to release the underlying connections.
func NewWithAMQP(ctx context.Context, dbPath, amqpURL, exchange string, logf func(format string, args ...any)) (*Server, error) {
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

	var publisher *rabbitmq.Publisher
	if strings.TrimSpace(amqpURL) != "" {
		publisher, err = rabbitmq.New(amqpURL, exchange, slog.New(slog.NewTextHandler(os.Stderr, nil)))
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("server: rabbitmq: %w", err)
		}
	}

	publishFn := func(r store.Message, record string) error {
		if publisher == nil {
			return nil
		}
		return publisher.Publish(rabbitmq.Message{
			Croid:     r.Croid,
			CroType:   r.CroType,
			CroValue:  r.CroValue,
			System:    r.System,
			CreatedAt: r.CreatedAt,
			Record:    record,
		})
	}

	return &Server{
		db:        conn,
		store:     store.NewWithPublish(db.New(conn), publishFn).WithLogger(logf),
		logf:      logf,
		publisher: publisher,
	}, nil
}

// Close releases the SQLite and RabbitMQ connections.
func (s *Server) Close() error {
	var err error
	if s.db != nil {
		err = s.db.Close()
	}
	if s.publisher != nil {
		if rerr := s.publisher.Close(); rerr != nil {
			if err == nil {
				err = rerr
			}
		}
	}
	return err
}

// Record adapts the store record for output shared by the CLI and the HTTP
// handlers, so they serialise identically.
type Record = store.Record

// Identity is the store's (cro_type, cro_value, system) identity, re-exported
// so the CLI can build one without importing the store package directly.
type Identity = store.Identity

// CroidResponse converts a minted record into the exact JSON body served by the
// HTTP handlers, so the CLI and POST /croid emit identical output.
func (s *Server) CroidResponse(r Record) any {
	return toResponse(r)
}

// MintCroid resolves or mints a CROID for the given identity using the exact
// same store path as POST /croid. It trims the identity fields exactly as the
// HTTP handler does before minting, so the CLI and POST /croid behave
// identically. It returns the record and whether a new CROID was minted (as
// opposed to returning a pre-existing one). An invalid identity returns an
// error, matching the 400 the HTTP handler returns.
func (s *Server) MintCroid(ctx context.Context, id Identity, record string) (Record, bool, error) {
	id.CroType = strings.TrimSpace(id.CroType)
	id.CroValue = strings.TrimSpace(id.CroValue)
	id.System = strings.TrimSpace(id.System)
	rec, err := s.store.Create(ctx, id, record)
	if err != nil {
		return Record{}, false, err
	}
	return rec, rec.Created, nil
}

// Handler returns the http.Handler exposing:
//
//	GET  /croid/{croid}  -> record (200) or 404
//	POST /croid          -> create (201) or dedupe returning existing (200)
//
// plus a liveness probe at GET /healthz and a service banner at GET /.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleRoot)
	mux.HandleFunc("GET /croid/{croid}", s.handleGet)
	mux.HandleFunc("POST /croid", s.handleCreate)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	return withLogging(mux, s.logf)
}

type croIDBody struct {
	CroType  string `json:"cro_type"`
	CroValue string `json:"cro_value"`
	System   string `json:"system"`
	Record   string `json:"record,omitempty"`
}

type croIDResponse struct {
	Croid     string `json:"croid"`
	CroType   string `json:"cro_type"`
	CroValue  string `json:"cro_value"`
	System    string `json:"system"`
	CreatedAt string `json:"created_at"`
}

type errorBody struct {
	Error string `json:"error"`
}

const maxBodyBytes = 2 * 1024 * 1024

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"service":   "croid",
		"endpoints": "POST /croid, GET /croid/{croid}",
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.db.PingContext(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, errorBody{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	c := r.PathValue("croid")
	record, err := s.store.GetByCroid(r.Context(), c)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, errorBody{Error: "croid not found"})
		return
	}
	if err != nil {
		s.logf("GET /croid/%s: %v", c, err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Error: "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, toResponse(record))
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	var body croIDBody
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err := dec.Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "invalid JSON body: " + err.Error()})
		return
	}

	identity := store.Identity{
		CroType:  strings.TrimSpace(body.CroType),
		CroValue: strings.TrimSpace(body.CroValue),
		System:   strings.TrimSpace(body.System),
	}

	record, err := s.store.Create(r.Context(), identity, body.Record)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: err.Error()})
		return
	}

	status := http.StatusOK
	if record.Created {
		status = http.StatusCreated
	}
	writeJSON(w, status, toResponse(record))
}

func toResponse(r store.Record) croIDResponse {
	return croIDResponse{
		Croid:     r.Croid,
		CroType:   r.CroType,
		CroValue:  r.CroValue,
		System:    r.System,
		CreatedAt: r.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

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
