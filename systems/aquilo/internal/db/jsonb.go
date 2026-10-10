package db

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// JSONB is a json.RawMessage alias that implements driver.Valuer
// for safe round-tripping of JSON through SQLite.
type JSONB json.RawMessage

// Value implements driver.Valuer, serializing JSONB to []byte.
func (j JSONB) Value() (driver.Value, error) {
	return []byte(json.RawMessage(j)), nil
}

// Scan implements sql.Scanner, accepting []byte or string
// and storing it as JSONB.
func (j *JSONB) Scan(src interface{}) error {
	var b []byte
	switch v := src.(type) {
	case []byte:
		b = make([]byte, len(v))
		copy(b, v)
	case string:
		b = []byte(v)
	default:
		return fmt.Errorf("jsonb: unsupported type %T", src)
	}
	*j = JSONB(json.RawMessage(b))
	return nil
}
