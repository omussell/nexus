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

	doi := strPtr(getStr(raw, "DOI"))
	abstract := strPtr(getStr(raw, "abstract"))
	publisher := strPtr(getStr(raw, "publisher"))
	docType := strPtr(getStr(raw, "type"))
	lang := strPtr(getStr(raw, "language"))
	volume := strPtr(getStr(raw, "volume"))
	issue := strPtr(getStr(raw, "issue"))
	page := strPtr(getStr(raw, "page"))
	doiAssertedBy := strPtr(getStr(raw, "doi-asserted-by"))

	titleArr, _ := getStringArray(raw, "title")
	title := ""
	if len(titleArr) > 0 {
		title = titleArr[0]
	}
	titlePtr := strPtr(title)

	groupTitle, _ := getStringArray(raw, "group_title")
	containerTitle, _ := getStringArray(raw, "container-title")
	issn, _ := getStringArray(raw, "ISSN")
	subject, _ := getStringArray(raw, "subject")

	author := getAuthors(raw, "author")
	funder := getFunders(raw, "funder")

	created := getDate(raw, "created")
	published := getDate(raw, "published-print")
	publishedOnline := getDate(raw, "published-online")
	issued := getDate(raw, "issued")
	updated := getDate(raw, "updated")

	createdPtr := strPtr(created)
	publishedPrintPtr := strPtr(published)
	publishedOnlinePtr := strPtr(publishedOnline)
	issuedPtr := strPtr(issued)
	updatedPtr := strPtr(updated)

	referenceCount, _ := getInt32(raw, "reference-count")
	isReferencedBy, _ := getInt32(raw, "is-referenced-by-count")

	t := record{
		DOI:               doi,
		Abstract:          abstract,
		Title:             titlePtr,
		GroupTitle:        groupTitle,
		Type:              docType,
		Publisher:         publisher,
		Created:           createdPtr,
		Published:         issuedPtr,
		Updated:           updatedPtr,
		AuthorName:        author.name,
		AuthorGiven:       author.given,
		AuthorFamily:      author.family,
		AuthorAffiliation: author.affiliation,
		AuthorID:          author.id,
		FunderName:        funder.name,
		FunderDOI:         funder.doi,
		ReferenceCount:    &referenceCount,
		IsReferencedBy:    &isReferencedBy,
		DOIAssertedBy:     doiAssertedBy,
		Language:          lang,
		Volume:            volume,
		Issue:             issue,
		Page:              page,
		ISSN:              issn,
		ContainerTitle:    containerTitle,
		PublishedPrint:    publishedPrintPtr,
		PublishedOnline:   publishedOnlinePtr,
		Subject:           subject,
	}

	buf := &bytes.Buffer{}
	writer := parquet.NewWriter(buf)
	if err := writer.Write(t); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type record struct {
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

func strPtr(s string) *string {
	return &s
}
