// Package server provides an HTTP interface for ROR matching against the
// vulcanus DuckDB database.
package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"

	"github.com/nexus/vulcanus/internal/match"
	"github.com/nexus/vulcanus/internal/match/country"
)

// Config holds the configuration for the matching server.
type Config struct {
	Host   string
	Port   int
	DBPath string
}

// Server wraps the matching server with HTTP handlers.
type Server struct {
	cfg       Config
	dbClient  *match.DuckDBClient
	countries []country.Country
	mux       *http.ServeMux
}

// New creates a new matching server with the given configuration.
func New(cfg Config) (*Server, error) {
	srv := &Server{
		cfg: cfg,
		mux: http.NewServeMux(),
	}

	// Load country data from resources directory
	resDir := filepath.Join(filepath.Dir(cfg.DBPath), "resources")
	if resDir == "" {
		resDir = "./resources"
	}

	countryFile := filepath.Join(resDir, "countries.txt")
	countries, err := country.Load(countryFile)
	if err != nil {
		log.Printf("Warning: could not load countries from %s: %v", countryFile, err)
	} else {
		srv.countries = countries
	}

	// Open DuckDB client
	if cfg.DBPath != "" {
		dbClient := match.NewDuckDBClient(cfg.DBPath, countries)
		count := dbClient.Count()
		log.Printf("Loaded %d organizations into DuckDB from %s", count, cfg.DBPath)
		srv.dbClient = dbClient
	}

	// Register routes
	srv.registerRoutes()

	return srv, nil
}

// registerRoutes sets up all HTTP handlers.
func (s *Server) registerRoutes() {
	// Health check
	s.mux.HandleFunc("/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// List tasks
	s.mux.HandleFunc("/tasks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(tasksResponse()))
	})

	// List strategies for a task
	s.mux.HandleFunc("/tasks/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if len(path) > 8 && path[7:] != "" {
			taskID := path[7:]
			if len(taskID) > 12 && taskID[len(taskID)-12:] == "/strategies" {
				taskID = taskID[:len(taskID)-12]
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(strategiesResponse(taskID)))
				return
			}
		}
		http.NotFound(w, r)
	})

	// Match endpoint (GET and POST)
	s.mux.HandleFunc("/match", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		q := r.URL.Query()
		task := cleanTaskID(q.Get("task"))
		inputData := q.Get("input")

		if r.Method == http.MethodPost {
			// Read body as input data
			buf := make([]byte, 1024*1024) // 1MB max
			n, err := r.Body.Read(buf)
			if err == nil && n > 0 {
				inputData = string(buf[:n])
			}
		}

		if task == "" || inputData == "" {
			http.Error(w, "Missing 'task' or 'input' query parameter", http.StatusBadRequest)
			return
		}

		// Run matching
		ir := match.New(inputData)
		foundCountries := match.FindCountries(inputData, s.countries, ir)

		var items interface{}
		if s.dbClient != nil {
			if task == "funder" {
				items = s.dbClient.MatchFunder(inputData, foundCountries)
			} else {
				items = s.dbClient.MatchAffiliation(inputData, foundCountries)
			}
		} else {
			items = []map[string]interface{}{}
		}

		itemsJSON, _ := json.Marshal(items)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{
			"status": "ok",
			"message_type": "matched-item-list",
			"message_version": "1.0.0",
			"message": {
				"items": %s,
				"target_data": null,
				"strategy": "vulcanus"
			}
		}`, string(itemsJSON))
	})
}

// Start starts the HTTP server on the configured host:port.
func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
	log.Printf("Listening on %s", addr)
	return http.ListenAndServe(addr, s)
}

// Close releases any resources held by the server.
func (s *Server) Close() error {
	if s.dbClient != nil {
		return s.dbClient.Close()
	}
	return nil
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func cleanTaskID(task string) string {
	// Remove "-matching" suffix if present
	if len(task) >= 9 && task[len(task)-9:] == "-matching" {
		return task[:len(task)-9]
	}
	return task
}

func tasksResponse() string {
	return `{
		"status": "ok",
		"message_type": "task-list",
		"message_version": "1.0.0",
		"message": {
			"items": [
				{"id": "reference", "description": "Matching bibliographic references to works, such as journal articles, conference papers, etc.", "default_strategy": "reference-sbmv"},
				{"id": "preprint", "description": "Matching journal articles to preprints.", "default_strategy": "preprint-sbmv"},
				{"id": "affiliation", "description": "Matching affiliations to ROR IDs.", "default_strategy": "affiliation-single-search"},
				{"id": "funder", "description": "Matching funders to ROR IDs.", "default_strategy": "funder-name-to-ror-search"},
				{"id": "other", "description": "A generic matching task.", "default_strategy": "N/A"},
				{"id": "healthcheck", "description": "used internally to check that things are working properly", "default_strategy": "healthcheck"}
			]
		}
	}`
}

func strategiesResponse(task string) string {
	// Simplified strategy listing
	_ = task
	return `{
		"status": "ok",
		"message_type": "strategy-list",
		"message_version": "1.0.0",
		"message": {
			"items": []
		}
	}`
}
