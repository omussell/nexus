package parquet

import (
	"bytes"
	"testing"

	"github.com/parquet-go/parquet-go"
)

func TestConvert_Basic(t *testing.T) {
	json := []byte(`{
		"DOI": "10.1/test",
		"type": "journal-article",
		"publisher": "Test Publisher",
		"title": ["Test Title"],
		"ISSN": ["1234-5678"],
		"reference-count": 5,
		"is-referenced-by-count": 10,
		"author": [
			{"given": "John", "family": "Doe", "affiliation": [{"name": "University"}]}
		],
		"funder": [
			{"name": "Funding Agency", "DOI": "10.13039/1234"}
		],
		"created": {"date-time": "2024-01-01T00:00:00Z"},
		"issued": {"date-time": "2024-06-01T00:00:00Z"},
		"container-title": ["Test Journal"],
		"language": "en",
		"volume": "10",
		"issue": "2",
		"page": "100-200",
		"doi-asserted-by": "publisher",
		"subject": ["Physics", "Chemistry"]
	}`)

	data, err := Convert(json)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("Convert returned empty data")
	}

	// Read back the parquet file to verify it is valid
	buf := bytes.NewReader(data)
	r, err := parquet.OpenFile(buf, int64(len(data)))
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}

	if r.NumRows() != 1 {
		t.Errorf("NumRows() = %d, want 1", r.NumRows())
	}

	// Check some column paths exist
	columns := r.Schema().Columns()
	if len(columns) == 0 {
		t.Fatal("expected columns in schema")
	}

	// Verify key column paths
	found := make(map[string]bool)
	for _, col := range columns {
		if len(col) > 0 {
			found[col[len(col)-1]] = true
		}
	}
	for _, key := range []string{"doi", "title", "type", "publisher", "reference_count", "is_referenced_by_count"} {
		if !found[key] {
			t.Errorf("expected column %q not found", key)
		}
	}
}

func TestConvert_Empty(t *testing.T) {
	json := []byte(`{}`)

	data, err := Convert(json)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("Convert returned empty data")
	}
}

func TestConvert_InvalidJSON(t *testing.T) {
	json := []byte(`not json`)

	_, err := Convert(json)
	if err == nil {
		t.Fatal("Convert should have returned an error for invalid JSON")
	}
}

func TestConvert_AuthorWithoutName(t *testing.T) {
	json := []byte(`{
		"DOI": "10.1/test2",
		"author": [
			{"given": "Jane", "family": "Smith"}
		]
	}`)

	data, err := Convert(json)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("Convert returned empty data")
	}
}

func TestConvert_DateFallback(t *testing.T) {
	json := []byte(`{
		"DOI": "10.1/test3",
		"created": "2024-01-01"
	}`)

	data, err := Convert(json)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("Convert returned empty data")
	}
}
