package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestQuery_Success(t *testing.T) {
	st, conn, err := createTestStore(t)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer conn.Close()

	if _, _, err := st.RecordMany(context.Background(), "7.json", []string{"10.1/thing"}); err != nil {
		t.Fatalf("record: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/query?doi=10.1/thing", nil)
	rr := httptest.NewRecorder()
	New(st, nil).Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rr.Code, rr.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got["file"] != "7.json" || got["doi"] != "10.1/thing" {
		t.Fatalf("body = %v, want file 7.json", got)
	}
}

func TestQuery_NotRecorded(t *testing.T) {
	st, conn, err := createTestStore(t)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer conn.Close()

	req := httptest.NewRequest(http.MethodGet, "/query?doi=10.1/never", nil)
	rr := httptest.NewRecorder()
	New(st, nil).Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
	var got map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got["error"] == "" {
		t.Fatalf("expected an error field, got %v", got)
	}
}

func TestQuery_MissingDoi(t *testing.T) {
	st, conn, err := createTestStore(t)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer conn.Close()

	req := httptest.NewRequest(http.MethodGet, "/query", nil)
	rr := httptest.NewRecorder()
	New(st, nil).Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func TestQuery_DuplicateDoiParam(t *testing.T) {
	st, conn, err := createTestStore(t)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer conn.Close()

	req := httptest.NewRequest(http.MethodGet, "/query?doi=a&doi=b", nil)
	rr := httptest.NewRecorder()
	New(st, nil).Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func TestQuery_MethodNotAllowed(t *testing.T) {
	st, conn, err := createTestStore(t)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer conn.Close()

	req := httptest.NewRequest(http.MethodPost, "/query?doi=10.1/x", nil)
	rr := httptest.NewRecorder()
	New(st, nil).Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rr.Code)
	}
}
