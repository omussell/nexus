// Package parquet converts Crossref work JSON into Parquet format.
package parquet

import (
	"bytes"
	"encoding/json"

	"github.com/parquet-go/parquet-go"
)

// Convert converts a single Crossref work JSON object into Parquet bytes.
// It extracts the most commonly queried fields and writes a single-record
// Parquet file in memory, returning it as []byte.
func Convert(jsonBytes []byte) ([]byte, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(jsonBytes, &raw); err != nil {
		return nil, err
	}
	r := NewRecord(raw)
	return MarshalRecord(&r)
}

// MarshalRecord serializes a single Record to parquet bytes.
func MarshalRecord(r *Record) ([]byte, error) {
	buf := &bytes.Buffer{}
	writer := parquet.NewWriter(buf)
	if err := writer.Write(r); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Record is a single row in the nauvis parquet file, representing a Crossref work.
type Record struct {
	DOI               *string  `parquet:"doi,optional"`
	Abstract          *string  `parquet:"abstract,optional"`
	Title             *string  `parquet:"title,optional"`
	GroupTitle        []string `parquet:"group_title"`
	Type              *string  `parquet:"type,optional"`
	Publisher         *string  `parquet:"publisher,optional"`
	Created           *string  `parquet:"created,optional"`
	Published         *string  `parquet:"published,optional"`
	Updated           *string  `parquet:"updated,optional"`
	AuthorName        []string `parquet:"author_name"`
	AuthorGiven       []string `parquet:"author_given"`
	AuthorFamily      []string `parquet:"author_family"`
	AuthorAffiliation []string `parquet:"author_affiliation"`
	AuthorID          []string `parquet:"author_id"`
	FunderName        []string `parquet:"funder_name"`
	FunderDOI         []string `parquet:"funder_doi"`
	ReferenceCount    *int32   `parquet:"reference_count,optional"`
	IsReferencedBy    *int32   `parquet:"is_referenced_by_count,optional"`
	DOIAssertedBy     *string  `parquet:"doi_asserted_by,optional"`
	Language          *string  `parquet:"language,optional"`
	Volume            *string  `parquet:"volume,optional"`
	Issue             *string  `parquet:"issue,optional"`
	Page              *string  `parquet:"page,optional"`
	ISSN              []string `parquet:"issn"`
	ContainerTitle    []string `parquet:"container_title"`
	PublishedPrint    *string  `parquet:"published_print,optional"`
	PublishedOnline   *string  `parquet:"published_online,optional"`
	Subject           []string `parquet:"subject"`
}

// NewRecord creates a Record from a Crossref work JSON object.
func NewRecord(raw map[string]json.RawMessage) Record {
	return Record{
		DOI:               strPtr(getStr(raw, "DOI")),
		Abstract:          strPtr(getStr(raw, "abstract")),
		Title:             strPtr(getStr(raw, "title")),
		GroupTitle:        getStrArray(raw, "group_title"),
		Type:              strPtr(getStr(raw, "type")),
		Publisher:         strPtr(getStr(raw, "publisher")),
		Created:           strPtr(getDate(raw, "created")),
		Published:         strPtr(getDate(raw, "issued")),
		Updated:           strPtr(getDate(raw, "updated")),
		AuthorName:        getAuthorField(raw, "author", "name"),
		AuthorGiven:       getAuthorField(raw, "author", "given"),
		AuthorFamily:      getAuthorField(raw, "author", "family"),
		AuthorAffiliation: getAuthorField(raw, "author", "affiliation"),
		AuthorID:          getAuthorField(raw, "author", "id"),
		FunderName:        getFunderField(raw, "funder", "name"),
		FunderDOI:         getFunderField(raw, "funder", "doi"),
		ReferenceCount:    int32Ptr(getInt64(raw, "reference-count")),
		IsReferencedBy:    int32Ptr(getInt64(raw, "is-referenced-by-count")),
		DOIAssertedBy:     strPtr(getStr(raw, "doi-asserted-by")),
		Language:          strPtr(getStr(raw, "language")),
		Volume:            strPtr(getStr(raw, "volume")),
		Issue:             strPtr(getStr(raw, "issue")),
		Page:              strPtr(getStr(raw, "page")),
		ISSN:              getStrArray(raw, "ISSN"),
		ContainerTitle:    getStrArray(raw, "container-title"),
		PublishedPrint:    strPtr(getDate(raw, "published-print")),
		PublishedOnline:   strPtr(getDate(raw, "published-online")),
		Subject:           getStrArray(raw, "subject"),
	}
}

// getStrArray extracts a string array from raw.
func getStrArray(raw map[string]json.RawMessage, key string) []string {
	val, ok := raw[key]
	if !ok {
		return nil
	}
	var arr []string
	if err := json.Unmarshal(val, &arr); err != nil {
		return nil
	}
	return arr
}

// --- helpers ---

func getStr(raw map[string]json.RawMessage, key string) string {
	val, ok := raw[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(val, &s); err != nil {
		return ""
	}
	return s
}

func getStringArray(raw map[string]json.RawMessage, key string) ([]string, bool) {
	val, ok := raw[key]
	if !ok {
		return nil, false
	}
	var arr []string
	if err := json.Unmarshal(val, &arr); err != nil {
		return nil, false
	}
	return arr, true
}

func getInt32(raw map[string]json.RawMessage, key string) (int32, bool) {
	val, ok := raw[key]
	if !ok {
		return 0, false
	}
	var n int64
	if err := json.Unmarshal(val, &n); err != nil {
		return 0, false
	}
	return int32(n), true
}

func getDate(raw map[string]json.RawMessage, key string) string {
	val, ok := raw[key]
	if !ok {
		return ""
	}
	var date struct {
		DateTime string `json:"date-time"`
	}
	if err := json.Unmarshal(val, &date); err == nil && date.DateTime != "" {
		return date.DateTime
	}
	// fallback: try raw string
	var s string
	if err := json.Unmarshal(val, &s); err == nil {
		return s
	}
	return ""
}

type authors struct {
	name, given, family, affiliation, id []string
}

func getAuthors(raw map[string]json.RawMessage, key string) authors {
	val, ok := raw[key]
	if !ok {
		return authors{}
	}
	var arr []struct {
		Name        string `json:"name"`
		Given       string `json:"given"`
		Family      string `json:"family"`
		Affiliation []struct {
			Name string `json:"name"`
		} `json:"affiliation"`
		Orchid string `json:"ORCID"`
	}
	if err := json.Unmarshal(val, &arr); err != nil {
		return authors{}
	}
	a := authors{
		name:        make([]string, 0, len(arr)),
		given:       make([]string, 0, len(arr)),
		family:      make([]string, 0, len(arr)),
		affiliation: make([]string, 0, len(arr)),
		id:          make([]string, 0, len(arr)),
	}
	for _, item := range arr {
		if item.Name != "" {
			a.name = append(a.name, item.Name)
		}
		if item.Given != "" {
			a.given = append(a.given, item.Given)
		}
		if item.Family != "" {
			a.family = append(a.family, item.Family)
		}
		if len(item.Affiliation) > 0 && item.Affiliation[0].Name != "" {
			a.affiliation = append(a.affiliation, item.Affiliation[0].Name)
		}
		if item.Orchid != "" {
			a.id = append(a.id, item.Orchid)
		}
	}
	return a
}

// getAuthorField extracts a specific field from author array items.
func getAuthorField(raw map[string]json.RawMessage, key, field string) []string {
	val, ok := raw[key]
	if !ok {
		return nil
	}
	var arr []struct {
		Name        string `json:"name"`
		Given       string `json:"given"`
		Family      string `json:"family"`
		Affiliation []struct {
			Name string `json:"name"`
		} `json:"affiliation"`
		Orchid string `json:"ORCID"`
	}
	if err := json.Unmarshal(val, &arr); err != nil {
		return nil
	}
	results := make([]string, 0, len(arr))
	for _, item := range arr {
		switch field {
		case "name":
			if item.Name != "" {
				results = append(results, item.Name)
			}
		case "given":
			if item.Given != "" {
				results = append(results, item.Given)
			}
		case "family":
			if item.Family != "" {
				results = append(results, item.Family)
			}
		case "affiliation":
			if len(item.Affiliation) > 0 && item.Affiliation[0].Name != "" {
				results = append(results, item.Affiliation[0].Name)
			}
		case "id":
			if item.Orchid != "" {
				results = append(results, item.Orchid)
			}
		}
	}
	return results
}

type funders struct {
	name []string
	doi  []string
}

func getFunders(raw map[string]json.RawMessage, key string) funders {
	val, ok := raw[key]
	if !ok {
		return funders{}
	}
	var arr []struct {
		Name string `json:"name"`
		DOI  string `json:"DOI"`
	}
	if err := json.Unmarshal(val, &arr); err != nil {
		return funders{}
	}
	f := funders{
		name: make([]string, 0, len(arr)),
		doi:  make([]string, 0, len(arr)),
	}
	for _, item := range arr {
		if item.Name != "" {
			f.name = append(f.name, item.Name)
		}
		if item.DOI != "" {
			f.doi = append(f.doi, item.DOI)
		}
	}
	return f
}

// getFunderField extracts a specific field from funder array items.
func getFunderField(raw map[string]json.RawMessage, key, field string) []string {
	val, ok := raw[key]
	if !ok {
		return nil
	}
	var arr []struct {
		Name string `json:"name"`
		DOI  string `json:"DOI"`
	}
	if err := json.Unmarshal(val, &arr); err != nil {
		return nil
	}
	results := make([]string, 0, len(arr))
	for _, item := range arr {
		switch field {
		case "name":
			if item.Name != "" {
				results = append(results, item.Name)
			}
		case "doi":
			if item.DOI != "" {
				results = append(results, item.DOI)
			}
		}
	}
	return results
}

// int32Ptr converts an int64 to *int32.
func int32Ptr(n int64) *int32 {
	if n == 0 {
		return nil
	}
	v := int32(n)
	return &v
}

// getInt64 extracts an int64 from raw.
func getInt64(raw map[string]json.RawMessage, key string) int64 {
	val, ok := raw[key]
	if !ok {
		return 0
	}
	var n int64
	if err := json.Unmarshal(val, &n); err != nil {
		return 0
	}
	return n
}

func strPtr(s string) *string {
	return &s
}
