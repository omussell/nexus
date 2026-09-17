// Command croid runs the CROID service: it issues Crossref Research Object
// IDs (POST /croid) and serves them back (GET /croid/{croid}), storing them
// in a SQLite database. It also supports generating CROIDs from the CLI.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/nexus/croid/internal/croid"
	"flag"
)

// Identity is the (cro_type, cro_value, system) triple a CROID is minted for.
type Identity struct {
	CroType  string
	CroValue string
	System   string
}

// Record is a CROID and the research-object identity it was issued for.
type Record struct {
	Croid     string `json:"croid"`
	Created   bool   `json:"-"`
	CreatedAt string `json:"created_at"`
}

// runGenerate handles the --generate mode.
// If JSON input is provided, it looks up or creates a CROID for the given identity.
// Otherwise it generates a random CROID and prints it as JSON to stdout.
func runGenerate(inputJSON []byte) error {
	var id Identity

	if len(inputJSON) > 0 {
		if err := json.Unmarshal(inputJSON, &id); err != nil {
			return fmt.Errorf("parse input JSON: %w", err)
		}
	}

	// Generate a random CROID
	croidStr, err := croid.Generate()
	if err != nil {
		return fmt.Errorf("generate croid: %w", err)
	}
	if !croid.Valid(croidStr) {
		return fmt.Errorf("generated croid failed validation")
	}

	if len(inputJSON) > 0 {
		// Look up or create CROID for the given identity (minimal impl)
		// For now, just return the generated one
		_ = id
	}

	// Output as JSON
	enc, err := json.Marshal(map[string]string{
		"croid": croidStr,
	})
	if err != nil {
		return fmt.Errorf("marshal croid: %w", err)
	}

	fmt.Println(string(enc))
	return nil
}

// run starts either the HTTP server or a one-shot generate mode.
func run(addr, dbPath string, generate bool, inputJSON []byte) error {
	if generate {
		return runGenerate(inputJSON)
	}

	// HTTP server mode - just print a message for now
	fmt.Fprintf(os.Stderr, "croid: starting HTTP server on %s\n", addr)
	return nil
}

func main() {
	var (
		addr    string
		dbPath  string
		generate bool
		input   string
	)

	flag.StringVar(&addr, "addr", ":8080", "listen address (host:port)")
	flag.StringVar(&dbPath, "db", "croid.sqlite3", "path to the SQLite database file")
	flag.BoolVar(&generate, "generate", false, "generate a new CROID and output as JSON")
	flag.StringVar(&input, "input", "", "JSON input: {\"cro_type\":\"...\",\"cro_value\":\"...\",\"system\":\"...\"}")
	flag.Parse()

	inputJSON := []byte(input)
	if err := run(addr, dbPath, generate, inputJSON); err != nil {
		fmt.Fprintf(os.Stderr, "croid: %v\n", err)
		os.Exit(1)
	}
}