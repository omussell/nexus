package server

import (
	"context"
	"testing"

	"github.com/nexus/nauvis/internal/store"
)

func createTestStore(t *testing.T) (*store.Store, interface{ Close() error }, error) {
	t.Helper()
	st, conn, err := store.Open(context.Background(), t.TempDir()+"/test.sqlite3")
	if err != nil {
		return nil, nil, err
	}
	return st, conn, nil
}
