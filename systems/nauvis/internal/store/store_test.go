package store

import (
	"context"
	"database/sql"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	st, conn, err := Open(context.Background(), t.TempDir()+"/test.sqlite3")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return st
}

func TestRecordMany_Basic(t *testing.T) {
	st := openTestStore(t)
	dupes, inserted, err := st.RecordMany(context.Background(), "0.json", []string{"10.1/a", "10.1/b"})
	if err != nil {
		t.Fatalf("RecordMany: %v", err)
	}
	if inserted != 2 {
		t.Fatalf("inserted %d, want 2", inserted)
	}
	if dupes != nil {
		t.Fatalf("dupes = %v, want none", dupes)
	}
}

func TestRecordMany_SkipsEmptyDOIs(t *testing.T) {
	st := openTestStore(t)
	_, inserted, err := st.RecordMany(context.Background(), "0.json", []string{"10.1/a", "", ""})
	if err != nil {
		t.Fatalf("RecordMany: %v", err)
	}
	if inserted != 1 {
		t.Fatalf("inserted %d, want 1", inserted)
	}
}

func TestRecordMany_DuplicateWithinFile(t *testing.T) {
	st := openTestStore(t)
	dupes, inserted, err := st.RecordMany(context.Background(), "0.json", []string{"10.1/a", "10.1/a"})
	if err != nil {
		t.Fatalf("RecordMany: %v", err)
	}
	if inserted != 1 {
		t.Fatalf("inserted %d, want 1", inserted)
	}
	if len(dupes) != 1 {
		t.Fatalf("dupes = %v, want 1 duplicate", dupes)
	}
}

func TestRecordMany_SkipsExistingDOIs(t *testing.T) {
	st := openTestStore(t)
	if _, _, err := st.RecordMany(context.Background(), "0.json", []string{"10.1/a"}); err != nil {
		t.Fatalf("first RecordMany: %v", err)
	}
	dupes, inserted, err := st.RecordMany(context.Background(), "1.json", []string{"10.1/a"})
	if err != nil {
		t.Fatalf("second RecordMany: %v", err)
	}
	if inserted != 0 {
		t.Fatalf("inserted %d, want 0", inserted)
	}
	if len(dupes) != 1 {
		t.Fatalf("dupes = %v, want 1 duplicate", dupes)
	}
}

func TestGetByDOI(t *testing.T) {
	st := openTestStore(t)
	if _, _, err := st.RecordMany(context.Background(), "3.json", []string{"10.1/lookup"}); err != nil {
		t.Fatalf("RecordMany: %v", err)
	}
	item, err := st.GetByDOI(context.Background(), "10.1/lookup")
	if err != nil {
		t.Fatalf("GetByDOI: %v", err)
	}
	if item.File != "3.json" || item.Doi != "10.1/lookup" {
		t.Fatalf("GetByDOI = %+v", item)
	}
}

func TestGetByDOI_NotRecorded(t *testing.T) {
	st := openTestStore(t)
	if _, err := st.GetByDOI(context.Background(), "10.1/nope"); err != sql.ErrNoRows {
		t.Fatalf("error = %v, want sql.ErrNoRows", err)
	}
}

func TestGetByDOI_OnlyFirstFileIsRecorded(t *testing.T) {
	st := openTestStore(t)
	if _, _, err := st.RecordMany(context.Background(), "1.json", []string{"10.1/shared"}); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, _, err := st.RecordMany(context.Background(), "2.json", []string{"10.1/shared"}); err != nil {
		t.Fatalf("second: %v", err)
	}
	item, err := st.GetByDOI(context.Background(), "10.1/shared")
	if err != nil {
		t.Fatalf("GetByDOI: %v", err)
	}
	if item.File != "1.json" {
		t.Fatalf("GetByDOI file = %q, want 1.json", item.File)
	}
}

func TestIsUniqueViolation(t *testing.T) {
	cases := map[string]bool{
		"UNIQUE constraint failed: items.doi":                true,
		"SQLITE_CONSTRAINT_UNIQUE: UNIQUE constraint failed": true,
		"sqlite_constraint failed":                           true,
		"something else":                                     false,
	}
	for input, want := range cases {
		if got := isUniqueViolation(errorString(input)); got != want {
			t.Errorf("isUniqueViolation(%q) = %v, want %v", input, got, want)
		}
	}
	if isUniqueViolation(nil) {
		t.Errorf("isUniqueViolation(nil) = true, want false")
	}
}

type errorString string

func (e errorString) Error() string { return string(e) }
