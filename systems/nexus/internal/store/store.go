package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/nexus/nexus/internal/db"
)

// Relationship represents a single relationship record.
type Relationship struct {
	ID           int64
	Source       string
	Target       string
	Relationship string
	Evidence     string
	CreatedAt    time.Time
}

// Store holds read-only relationship type metadata. These maps are bounded
// (the relationship_types table holds ~100 rows) and never grow.
// CROID strings are resolved via DB queries at runtime — no unbounded
// in-memory cache.
type Store struct {
	relByID   map[int64]string // relationship type id → name
	relByName map[string]int64 // name → id

	conn *sql.DB
	q    *db.Queries
}

// New builds a Store over the given *sql.DB and sqlc Queries handle, pre-loading
// all relationship types into memory. CROID strings are resolved on demand
// via DB lookup, so the store does not grow unbounded at runtime.
func New(conn *sql.DB, q *db.Queries) (*Store, error) {
	typeRows, err := q.GetRelationshipTypeIDs(context.Background())
	if err != nil {
		return nil, fmt.Errorf("load relationship types: %w", err)
	}
	relByID := make(map[int64]string, len(typeRows))
	relByName := make(map[string]int64, len(typeRows))
	for _, tr := range typeRows {
		relByID[tr.ID] = tr.Relationship
		relByName[tr.Relationship] = tr.ID
	}

	return &Store{
		relByID:   relByID,
		relByName: relByName,
		conn:      conn,
		q:         q,
	}, nil
}

// UpsertCroid inserts a CROID into the cro_ids table (upsert) and returns its
// integer id. If the CROID already exists, the existing id is returned.
func (s *Store) UpsertCroid(ctx context.Context, croid, system string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	// Attempt INSERT; duplicates are silently ignored (unique constraint).
	err := s.q.UpsertCroidInsert(ctx, db.UpsertCroidInsertParams{
		Croid:     croid,
		System:    sql.NullString{String: system, Valid: system != ""},
		CreatedAt: now,
	})
	if err != nil {
		return 0, fmt.Errorf("upsert croid: %w", err)
	}

	// Read the id back (covers both newly inserted and pre-existing).
	cro, err := s.q.GetCroidByCroid(ctx, croid)
	if err != nil {
		return 0, fmt.Errorf("get croid %s: %w", croid, err)
	}
	return cro.ID, nil
}

// Insert adds a new relationship if (source, target, relationship) does not
// already exist. Returns the row id, a bool indicating if it was newly
// inserted (vs already present), and an error.
func (s *Store) Insert(ctx context.Context, r Relationship) (int64, bool, error) {
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	tq := s.q.WithTx(tx)

	now := time.Now().UTC().Format(time.RFC3339)

	// Upsert both CROIDs (no caching — DB index lookup is O(log n) ~1ms).
	sourceID, err := s.upsertCroidTx(ctx, tq, r.Source, "nexus")
	if err != nil {
		return 0, false, fmt.Errorf("upsert source croid: %w", err)
	}
	targetID, err := s.upsertCroidTx(ctx, tq, r.Target, "nexus")
	if err != nil {
		return 0, false, fmt.Errorf("upsert target croid: %w", err)
	}

	// Upsert relationship type and resolve its id.
	err = tq.InsertRelationshipType(ctx, db.InsertRelationshipTypeParams{
		Relationship: r.Relationship,
		CreatedAt:    now,
	})
	if err != nil {
		return 0, false, fmt.Errorf("upsert relationship type %q: %w", r.Relationship, err)
	}
	relTypeID, err := tq.GetRelationshipTypeID(ctx, r.Relationship)
	if err != nil {
		return 0, false, fmt.Errorf("resolve relationship type %q: %w", r.Relationship, err)
	}
	// Cache the newly discovered relationship type so ListByRelationship works
	// on the next call (bounded to ~100 entries total).
	s.relByID[relTypeID] = r.Relationship
	s.relByName[r.Relationship] = relTypeID

	// Check if relationship already exists (composite index, O(log n)).
	if id, err := tq.RelationshipExists(ctx, db.RelationshipExistsParams{
		SourceCroidID:      sourceID,
		TargetCroidID:      targetID,
		RelationshipTypeID: relTypeID,
	}); err == nil && id != 0 {
		if err := tx.Commit(); err != nil {
			return 0, false, err
		}
		return id, false, nil
	}

	// Insert the relationship.
	if err := tq.InsertRelationship(ctx, db.InsertRelationshipParams{
		SourceCroidID:      sourceID,
		TargetCroidID:      targetID,
		RelationshipTypeID: relTypeID,
		CreatedAt:          now,
	}); err != nil {
		return 0, false, fmt.Errorf("insert relationship: %w", err)
	}

	// Capture the relationship id inside the transaction.
	var relID int64
	if err := tx.QueryRowContext(ctx, "SELECT id FROM relationships WHERE source_croid_id=? AND target_croid_id=? AND relationship_type_id=?", sourceID, targetID, relTypeID).Scan(&relID); err != nil {
		return 0, false, fmt.Errorf("capture relationship id: %w", err)
	}

	// Insert evidence if provided.
	if r.Evidence != "" {
		if err := tq.InsertEvidence(ctx, db.InsertEvidenceParams{
			RelationshipID: relID,
			Evidence:       sql.NullString{String: r.Evidence, Valid: true},
		}); err != nil {
			return 0, false, fmt.Errorf("insert evidence: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, false, err
	}

	return relID, true, nil
}

// upsertCroidTx operates UpsertCroid within a transaction (no caching).
func (s *Store) upsertCroidTx(ctx context.Context, q *db.Queries, croid, system string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	err := q.UpsertCroidInsert(ctx, db.UpsertCroidInsertParams{
		Croid:     croid,
		System:    sql.NullString{String: system, Valid: system != ""},
		CreatedAt: now,
	})
	if err != nil {
		return 0, fmt.Errorf("upsert croid: %w", err)
	}

	row, err := q.GetCroidByCroid(ctx, croid)
	if err != nil {
		return 0, fmt.Errorf("get croid %s: %w", croid, err)
	}
	return row.ID, nil
}

// ListBySource returns all relationships for a given source CROID.
func (s *Store) ListBySource(ctx context.Context, source string) ([]Relationship, error) {
	sourceID, err := s.upsertAndResolveCroid(ctx, source)
	if err != nil {
		return nil, err
	}

	rows, err := s.q.ListBySourceCroidID(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	return rowsToListSource(rows, s.relByID)
}

// upsertAndResolveCroid ensures a CROID exists in cro_ids and returns its id.
func (s *Store) upsertAndResolveCroid(ctx context.Context, croid string) (int64, error) {
	id, err := s.q.GetCroidID(ctx, croid)
	if err == nil {
		return id, nil
	}
	// CROID not found — upsert and resolve.
	if _, err := s.UpsertCroid(ctx, croid, "nexus"); err != nil {
		return 0, err
	}
	id, err = s.q.GetCroidID(ctx, croid)
	if err != nil {
		return 0, fmt.Errorf("resolve croid %s after upsert: %w", croid, err)
	}
	return id, nil
}

// ListByRelationship returns all relationships matching the given type name.
func (s *Store) ListByRelationship(ctx context.Context, rel string) ([]Relationship, error) {
	relID, ok := s.relByName[rel]
	if !ok {
		return nil, fmt.Errorf("unknown relationship type: %s", rel)
	}

	rows, err := s.q.ListByRelationshipTypeID(ctx, relID)
	if err != nil {
		return nil, err
	}
	return rowsToListRel(rows, s.relByID)
}

// ListAll returns every relationship, ordered by ID.
func (s *Store) ListAll(ctx context.Context) ([]Relationship, error) {
	rows, err := s.q.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	return rowsToListAll(rows, s.relByID)
}

// rowsToListAll adapts ListAllRow (from the JOIN query) into Relationship.
func rowsToListAll(rows []db.ListAllRow, relByID map[int64]string) ([]Relationship, error) {
	out := make([]Relationship, 0, len(rows))
	for _, r := range rows {
		t, _ := time.Parse(time.RFC3339, r.CreatedAt)
		out = append(out, Relationship{
			ID:           r.ID,
			Source:       r.SourceCroid.String,
			Target:       r.TargetCroid.String,
			Relationship: relByID[r.RelationshipTypeID],
			CreatedAt:    t,
			Evidence:     r.Evidence.String,
		})
	}
	return out, nil
}

// rowsToListSource adapts ListBySourceCroidIDRow.
func rowsToListSource(rows []db.ListBySourceCroidIDRow, relByID map[int64]string) ([]Relationship, error) {
	out := make([]Relationship, 0, len(rows))
	for _, r := range rows {
		t, _ := time.Parse(time.RFC3339, r.CreatedAt)
		out = append(out, Relationship{
			ID:           r.ID,
			Source:       r.SourceCroid.String,
			Target:       r.TargetCroid.String,
			Relationship: relByID[r.RelationshipTypeID],
			CreatedAt:    t,
			Evidence:     r.Evidence.String,
		})
	}
	return out, nil
}

// rowsToListRel adapts ListByRelationshipTypeIDRow.
func rowsToListRel(rows []db.ListByRelationshipTypeIDRow, relByID map[int64]string) ([]Relationship, error) {
	out := make([]Relationship, 0, len(rows))
	for _, r := range rows {
		t, _ := time.Parse(time.RFC3339, r.CreatedAt)
		out = append(out, Relationship{
			ID:           r.ID,
			Source:       r.SourceCroid.String,
			Target:       r.TargetCroid.String,
			Relationship: relByID[r.RelationshipTypeID],
			CreatedAt:    t,
			Evidence:     r.Evidence.String,
		})
	}
	return out, nil
}

func (s *Store) GetByTarget(ctx context.Context, target string) (Relationship, error) {
	all, err := s.ListAll(ctx)
	if err != nil {
		return Relationship{}, err
	}
	for _, r := range all {
		if r.Target == target {
			return r, nil
		}
	}
	return Relationship{}, sql.ErrNoRows
}
