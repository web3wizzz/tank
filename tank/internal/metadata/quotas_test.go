package metadata

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestUploadLeaseAcrossConnectionsAndStaleRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tank.sqlite")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	first, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	token, err := first.AcquireUploadLease(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.AcquireUploadLease(ctx); !errors.Is(err, ErrUploadBusy) {
		t.Fatal("concurrent coordinator bypassed upload admission")
	}
	if _, err := first.db.ExecContext(ctx, "UPDATE upload_leases SET lease_until=1"); err != nil {
		t.Fatal(err)
	}
	replacement, err := second.AcquireUploadLease(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.ReleaseUploadLease(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := first.AcquireUploadLease(ctx); !errors.Is(err, ErrUploadBusy) {
		t.Fatal("stale release erased the replacement lease")
	}
	if err := second.ReleaseUploadLease(ctx, replacement); err != nil {
		t.Fatal(err)
	}
	next, err := first.AcquireUploadLease(ctx)
	if err != nil {
		t.Fatal("released lease did not restore admission")
	}
	first.ReleaseUploadLease(ctx, next)
	if _, err := first.AcquireUploadLease(context.Background()); err == nil {
		t.Fatal("unbounded upload lease accepted")
	}
}
func TestStorageUsageCountsSharedAndRepeatedFilesCorrectly(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "tank.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	alice, _, err := store.CreateAccessKey(ctx, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	bob, _, err := store.CreateAccessKey(ctx, "Bob")
	if err != nil {
		t.Fatal(err)
	}
	m := filenameTestManifest(t)
	if err := store.Save(ctx, m); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{alice.PrincipalID, alice.PrincipalID, bob.PrincipalID} {
		if err := store.GrantFileAccess(ctx, id, m.FileID, "private.pdf"); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{alice.PrincipalID, bob.PrincipalID} {
		usage, err := store.StorageUsage(ctx, id)
		if err != nil || usage.UserBytes != m.Size || usage.TotalBytes != m.Size {
			t.Fatal("shared or repeated file was charged incorrectly")
		}
	}
	usage, err := store.StorageUsage(ctx, "missing")
	if err != nil || usage.UserBytes != 0 || usage.TotalBytes != m.Size {
		t.Fatal("user storage accounting leaked across principals")
	}
}

func TestSchemaFiveQuotaMigrationPreservesCredentialsAndFileAccess(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tank.sqlite")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	key, token, err := store.CreateAccessKey(ctx, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	manifest := filenameTestManifest(t)
	if err := store.Save(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	if err := store.GrantFileAccess(ctx, key.PrincipalID, manifest.FileID, "private.pdf"); err != nil {
		t.Fatal(err)
	}
	if err := store.EnqueueRepair(ctx, manifest.FileID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, "DROP TABLE upload_leases; PRAGMA user_version=5"); err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	user, err := store.AuthenticateAccessKey(ctx, token)
	if err != nil || user.ID != key.PrincipalID {
		t.Fatal("schema migration changed a credential")
	}
	access, err := store.LoadFileAccess(ctx, user.ID, manifest.FileID)
	if err != nil || access.Filename != "private.pdf" || access.Size != manifest.Size {
		t.Fatal("schema migration changed file permissions or bytes")
	}
	var jobs int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM repair_jobs WHERE file_id=?", manifest.FileID).Scan(&jobs); err != nil || jobs != 1 {
		t.Fatal("schema migration lost a durable repair job")
	}
	usage, err := store.StorageUsage(ctx, user.ID)
	if err != nil || usage.UserBytes != manifest.Size || usage.TotalBytes != manifest.Size {
		t.Fatal("schema migration lost quota accounting")
	}
}
