package metadata

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRestoreReaderNeverMigratesOrWrites(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.sqlite")
	store, e := Open(ctx, path)
	if e != nil {
		t.Fatal(e)
	}
	m := fixture(t)
	if e = store.Save(ctx, m); e != nil {
		t.Fatal(e)
	}
	if e = store.EnqueueRegistration(ctx, m.FileID, "5042002:fixture:account"); e != nil {
		t.Fatal(e)
	}
	snapshot := filepath.Join(t.TempDir(), "snapshot.sqlite")
	if e = store.Backup(ctx, snapshot); e != nil {
		t.Fatal(e)
	}
	store.Close()
	before, e := os.ReadFile(snapshot)
	if e != nil {
		t.Fatal(e)
	}
	reader, e := OpenRestoreReadOnly(ctx, snapshot)
	if e != nil {
		t.Fatal(e)
	}
	state, e := reader.RestoreSnapshot(ctx)
	if e != nil || len(state.Jobs) != 1 || state.Jobs[0].Manifest.FileID != m.FileID {
		t.Fatal("snapshot export failed")
	}
	if e = reader.Save(ctx, fixture(t)); e == nil {
		t.Fatal("read-only restore reader allowed writes")
	}
	reader.Close()
	after, e := os.ReadFile(snapshot)
	if e != nil || string(before) != string(after) {
		t.Fatal("restore validation changed snapshot")
	}
	missing := filepath.Join(t.TempDir(), "missing.sqlite")
	if _, e = OpenRestoreReadOnly(ctx, missing); !errors.Is(e, ErrRestoreState) {
		t.Fatal("missing source accepted")
	}
	if _, e = os.Stat(missing); !os.IsNotExist(e) {
		t.Fatal("missing source created")
	}
	old, e := Open(ctx, path)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = old.db.ExecContext(ctx, "PRAGMA user_version=7"); e != nil {
		t.Fatal(e)
	}
	old.Close()
	if _, e = OpenRestoreReadOnly(ctx, path); !errors.Is(e, ErrRestoreState) {
		t.Fatal("older schema migrated or accepted")
	}
}
