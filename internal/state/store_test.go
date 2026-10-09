package state

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOpenConfiguresSQLiteForConcurrentDashboardReads(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var journalMode string
	if err := store.db.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}

	var busyTimeout int
	if err := store.db.QueryRow(`PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
		t.Fatal(err)
	}
	if busyTimeout != 10000 {
		t.Fatalf("busy_timeout = %d, want 10000", busyTimeout)
	}

	if err := store.Save(context.Background(), Record{Hash: "one", Name: "test", PayloadStatus: "ok"}); err != nil {
		t.Fatal(err)
	}
	if records, err := store.List(context.Background(), 10); err != nil || len(records) != 1 {
		t.Fatalf("List() = %d records, %v", len(records), err)
	}
}
