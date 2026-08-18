// Command nauvis extracts the Crossref snapshot dataset.
//
// It reads the gzip-compressed, JSON-L formatted input files (*.json.gz) from
// an input directory and, for each one, decompresses the payload, validates
// that it is well-formed JSON, and writes it back out as a plain .json file
// with byte-for-byte identical content to the decompressed source.
//
// The decompressed bytes are written unchanged on purpose: no Go types are
// applied to the data and nothing is re-serialized, so every value (and the
// original formatting) is preserved exactly as it appears in the source file.
//
// Thousands of input files are processed in parallel across a pool of workers.
package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Config holds the resolved command-line options.
type Config struct {
	// InDir is the directory containing the *.json.gz input files.
	InDir string
	// OutDir is the directory the decompressed .json files are written to.
	OutDir string
	// Jobs is the number of worker goroutines. If 0, len(GOMAXPROCS(0)) is used.
	Jobs int
}

// Result is the outcome of processing a single input file.
type Result struct {
	// Name is the logical name of the file (the input basename without .gz).
	Name string
	// OutPath is where the output was written (empty on error).
	OutPath string
	// Bytes is the decompressed output size in bytes.
	Bytes int64
	// Err is non-nil if the file could not be read, decompressed, or written.
	Err error
}

// ProcessFile decompresses one .json.gz file, validates the JSON, and writes
// the decompressed bytes to outPath unchanged. Returns the written size.
func ProcessFile(inPath, outPath string) (int64, error) {
	in, err := os.Open(inPath)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", inPath, err)
	}
	defer in.Close()

	gz, err := gzip.NewReader(in)
	if err != nil {
		return 0, fmt.Errorf("gzip %s: %w", inPath, err)
	}
	defer gz.Close()

	// Read the full decompressed payload once so we can both validate it and
	// write it back out byte-for-byte.
	buf, err := io.ReadAll(gz)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", inPath, err)
	}

	// Validate that the payload is well-formed JSON. No types/structs are used;
	// json.Valid only checks syntactic correctness without interpreting values.
	if !jsonValid(buf) {
		return 0, fmt.Errorf("%s: decompressed payload is not valid JSON", inPath)
	}

	// Write the exact decompressed bytes to the output file.
	if err := atomicWrite(outPath, buf, 0o644); err != nil {
		return 0, fmt.Errorf("write %s: %w", outPath, err)
	}
	return int64(len(buf)), nil
}

// Run processes every input file in inDir in parallel and writes the
// corresponding .json files into outDir.
func Run(cfg Config) (int, int, error) {
	files, err := listInputs(cfg.InDir)
	if err != nil {
		return 0, 0, err
	}

	jobs := cfg.Jobs
	if jobs <= 0 {
		jobs = runtime.GOMAXPROCS(0)
	}
	if jobs < 1 {
		jobs = 1
	}

	work := make(chan string)
	results := make(chan Result, len(files))

	var wg sync.WaitGroup
	for i := 0; i < jobs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for inPath := range work {
				results <- processOne(cfg, inPath)
			}
		}()
	}

	// Feed input files to workers.
	go func() {
		defer close(work)
		for _, p := range files {
			work <- p
		}
	}()

	// Close results once all workers are done.
	go func() {
		wg.Wait()
		close(results)
	}()

	var ok, failed int
	for res := range results {
		if res.Err != nil {
			failed++
			log.Printf("[FAIL] %s: %v", res.Name, res.Err)
			continue
		}
		ok++
		log.Printf("[ OK ] %s -> %s (%d bytes)", res.Name, res.OutPath, res.Bytes)
	}

	if failed > 0 {
		return ok, failed, fmt.Errorf("%d of %d input files failed", failed, len(files))
	}
	return ok, 0, nil
}

// processOne runs the full pipeline for a single input file.
func processOne(cfg Config, inPath string) Result {
	name := baseName(inPath)
	outPath := filepath.Join(cfg.OutDir, name)
	bytesWritten, err := ProcessFile(inPath, outPath)
	return Result{Name: name, OutPath: outPath, Bytes: bytesWritten, Err: err}
}

// listInputs returns an ordered list of every input file in dir. A file
// qualifies if its name ends in ".json.gz" (e.g. "5.json.gz") or ".json"
// (e.g. "10.json").
func listInputs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".json.gz") || strings.HasSuffix(name, ".json") {
			files = append(files, filepath.Join(dir, name))
		}
	}
	return files, nil
}

// baseName derives the output filename (without a trailing .gz) for a given
// input path, e.g. "/data/5.json.gz" -> "5.json".
func baseName(inPath string) string {
	base := filepath.Base(inPath)
	return strings.TrimSuffix(base, ".gz")
}

// jsonValid reports whether b is syntactically valid JSON.
func jsonValid(b []byte) bool {
	return json.Valid(b)
}

// atomicWrite writes data to path, creating the parent directory if needed,
// writing to a temp file in the same directory, then renaming into place.
func atomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func main() {
	cfg := Config{
		InDir:  "data",
		OutDir: "out",
		Jobs:   0,
	}
	flag.StringVar(&cfg.InDir, "in", cfg.InDir, "directory containing *.json.gz input files")
	flag.StringVar(&cfg.OutDir, "out", cfg.OutDir, "directory to write decompressed *.json files")
	flag.IntVar(&cfg.Jobs, "jobs", cfg.Jobs, "number of parallel workers (default GOMAXPROCS)")
	flag.Parse()

	log.SetOutput(bufio.NewWriter(os.Stderr))
	log.SetFlags(0)

	ok, failed, err := Run(cfg)
	log.Printf("done: %d ok, %d failed", ok, failed)
	if err != nil {
		log.Printf("nauvis: %v", err)
		os.Exit(1)
	}
}
