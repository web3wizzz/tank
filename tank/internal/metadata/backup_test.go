package metadata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBackupRestoresPrivateAuthorizationAndJobs(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	source := filepath.Join(directory, "live.sqlite")
	store, err := Open(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	alice, token, err := store.CreateAccessKey(ctx, "backup user")
	if err != nil {
		t.Fatal(err)
	}
	bob, _, err := store.CreateAccessKey(ctx, "other user")
	if err != nil {
		t.Fatal(err)
	}
	manifest := fixture(t)
	if err := store.Save(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveFilename(ctx, manifest.FileID, "encrypted.tankenc"); err != nil {
		t.Fatal(err)
	}
	if err := store.GrantFileAccess(ctx, alice.PrincipalID, manifest.FileID, "encrypted.tankenc"); err != nil {
		t.Fatal(err)
	}
	if err := store.EnqueueRepair(ctx, manifest.FileID); err != nil {
		t.Fatal(err)
	}
	target := "31337:test-contract:test-registrant"
	if err := store.EnqueueRegistration(ctx, manifest.FileID, target); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(directory, "snapshot 'quoted?.sqlite")
	if err := BackupDatabase(ctx, source, snapshot); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(snapshot)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("backup is not private")
	}
	restored, err := Open(ctx, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if user, err := restored.AuthenticateAccessKey(ctx, token); err != nil || user.ID != alice.PrincipalID {
		t.Fatal("backup lost credential or identity")
	}
	if access, err := restored.LoadFileAccess(ctx, alice.PrincipalID, manifest.FileID); err != nil || access.Filename != "encrypted.tankenc" {
		t.Fatal("backup lost permission or filename")
	}
	if _, err := restored.LoadFileAccess(ctx, bob.PrincipalID, manifest.FileID); !errors.Is(err, ErrAccessDenied) {
		t.Fatal("restore leaked cross-user access")
	}
	if _, err := restored.ClaimRepair(ctx, time.Minute); err != nil {
		t.Fatal("backup lost durable repair job")
	}
	if _, err := restored.ClaimRegistration(ctx, target, time.Minute); err != nil {
		t.Fatal("backup lost registration job")
	}
	if loaded, err := restored.Load(ctx, manifest.FileID); err != nil || loaded.FileID != manifest.FileID {
		t.Fatal("backup lost manifest")
	}
	if err := store.RevokeAccessKey(ctx, alice.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := restored.AuthenticateAccessKey(ctx, token); err != nil {
		t.Fatal("snapshot unexpectedly changed with live database")
	}
	// Restore operators must reconcile revocations since the snapshot, as documented.
}

func TestBackupConcurrentWritersAndRefusedDestinations(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	source := filepath.Join(directory, "live.sqlite")
	store, err := Open(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	writer, err := Open(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	finished := make(chan error, 1)
	first := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			m := fixture(t)
			m.FileID = fmt.Sprintf("%064x", i+1)
			body, _ := json.Marshal(m)
			tx, err := writer.db.BeginTx(ctx, nil)
			if err != nil {
				finished <- err
				return
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO manifests(file_id,body) VALUES(?,?)", m.FileID, string(body)); err == nil {
				_, err = tx.ExecContext(ctx, "INSERT INTO file_names(file_id,filename) VALUES(?,?)", m.FileID, "snapshot fixture")
			}
			if err != nil {
				tx.Rollback()
				finished <- err
				return
			}
			if err = tx.Commit(); err != nil {
				finished <- err
				return
			}
			if i == 0 {
				close(first)
			}
		}
		finished <- nil
	}()
	<-first
	snapshot := filepath.Join(directory, "snapshot.sqlite")
	if err := store.Backup(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	restored, err := Open(ctx, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var manifests, names int
	if err := restored.db.QueryRowContext(ctx, "SELECT count(*) FROM manifests").Scan(&manifests); err != nil {
		t.Fatal(err)
	}
	if err := restored.db.QueryRowContext(ctx, "SELECT count(*) FROM file_names").Scan(&names); err != nil {
		t.Fatal(err)
	}
	if manifests < 1 || manifests != names {
		t.Fatal("snapshot broke a writer transaction")
	}
	before, err := os.ReadFile(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Backup(ctx, snapshot); err == nil {
		t.Fatal("existing destination overwritten")
	}
	after, _ := os.ReadFile(snapshot)
	if string(before) != string(after) {
		t.Fatal("existing snapshot changed")
	}
	link := filepath.Join(directory, "symlink.sqlite")
	if err := os.Symlink(snapshot, link); err != nil {
		t.Fatal(err)
	}
	if err := store.Backup(ctx, link); err == nil {
		t.Fatal("symlink destination accepted")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	cancelPath := filepath.Join(directory, "canceled.sqlite")
	if err := store.Backup(canceled, cancelPath); err == nil {
		t.Fatal("canceled backup published")
	}
	if _, err := os.Lstat(cancelPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed backup left published data")
	}
	staging, _ := filepath.Glob(filepath.Join(directory, ".tank-backup-*"))
	if len(staging) != 0 {
		t.Fatal("owned staging files leaked")
	}
}

func TestBackupDoesNotMigrateSource(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	source := filepath.Join(directory, "old.sqlite")
	store, err := Open(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, "DROP TABLE registration_transactions; DROP TABLE upload_leases; PRAGMA user_version=5"); err != nil {
		t.Fatal(err)
	}
	store.Close()
	if err := BackupDatabase(ctx, source, filepath.Join(directory, "before-upgrade.sqlite")); err != nil {
		t.Fatal(err)
	}
	// Open read-only directly: opening the Store would migrate the source.
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: source, RawQuery: "mode=ro"}).String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 5 {
		t.Fatal("backup migrated source")
	}
}
