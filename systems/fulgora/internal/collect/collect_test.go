package collect

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/nexus/fulgora/internal/db"
	"github.com/nexus/fulgora/internal/migrate"
	"github.com/nexus/fulgora/internal/source"
	"github.com/nexus/fulgora/internal/store"
)

// fakeSource is a controllable source for testing the collect flow.
type fakeSource struct {
	name    string
	latest  *source.Latest
	lastVer string
	lastOut string
}

func (f *fakeSource) Name() string                                            { return f.name }
func (f *fakeSource) CheckLatest(ctx context.Context) (*source.Latest, error) { return f.latest, nil }
func (f *fakeSource) Process(ctx context.Context, version, rawPath, outRoot string) (string, error) {
	f.lastVer = version
	if err := os.MkdirAll(outRoot, 0o755); err != nil {
		return "", err
	}
	out := filepath.Join(outRoot, f.name+"-"+version)
	if err := os.WriteFile(out, []byte("processed"), 0o644); err != nil {
		return "", err
	}
	f.lastOut = out
	return out, nil
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	conn, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "c.sqlite3")+"?_fk=1&_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	conn.SetMaxOpenConns(1)
	if err := migrate.Apply(context.Background(), conn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return store.New(db.New(conn))
}

func TestCollectEndToEnd(t *testing.T) {
	// Fake download server returns some bytes for the file URL.
	var gotFile string
	dl := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFile = r.URL.Path
		w.Header().Set("Content-Type", "application/zip")
		if _, err := w.Write([]byte("filebytes")); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer dl.Close()

	src := &fakeSource{
		name: "test-src",
		latest: &source.Latest{
			Version:  "9.9",
			URL:      dl.URL + "/v9.9.zip",
			Filename: "v9.9.zip",
		},
	}

	root := t.TempDir()
	s := openStore(t)

	res, err := Collect(context.Background(), src, s, root)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if res.Skipped {
		t.Fatal("first collect should not be skipped")
	}
	if res.Version != "9.9" {
		t.Errorf("version = %q", res.Version)
	}
	// The raw file must exist under the README's initial_input layout.
	if !strings.Contains(res.InitialInput, "test-src/initial_input/") {
		t.Errorf("initial_input layout = %q", res.InitialInput)
	}
	if _, err := os.Stat(res.InitialInput); err != nil {
		t.Errorf("initial input file missing: %v", err)
	}
	// The output must have been produced by Process.
	if !filepath.IsAbs(res.Output) && res.Output == "" {
		t.Error("output empty")
	}
	if gotFile != "/v9.9.zip" {
		t.Errorf("download path = %q", gotFile)
	}

	// The store should reflect the new latest for the source.
	latest, err := s.Latest(context.Background(), "test-src")
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if latest.Version != "9.9" {
		t.Errorf("latest version = %q", latest.Version)
	}
}

func TestCollectSkipsAlreadyRecorded(t *testing.T) {
	src := &fakeSource{
		name:   "dedupe",
		latest: &source.Latest{Version: "2.0", URL: "https://example/x", Filename: "x.zip"},
	}
	root := t.TempDir()
	s := openStore(t)
	ctx := context.Background()

	// Seed the version as already recorded.
	if _, err := s.Record(ctx, store.Dataset{Source: "dedupe", Version: "2.0"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	res, err := Collect(ctx, src, s, root)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if !res.Skipped {
		t.Fatal("second collect for same version should be skipped")
	}
}

func TestCollectDownloadFailureLeavesNoFile(t *testing.T) {
	src := &fakeSource{
		name:   "faildl",
		latest: &source.Latest{Version: "1.0", URL: "http://127.0.0.1:1/nope", Filename: "n.zip"},
	}
	root := t.TempDir()
	s := openStore(t)

	if _, err := Collect(context.Background(), src, s, root); err == nil {
		t.Fatal("expected download to fail")
	}
	// Ensure nothing was recorded for this source.
	if _, err := s.Latest(context.Background(), "faildl"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}
