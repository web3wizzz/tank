package metadata

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRegistrationQueueRecovery(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tank.sqlite")
	target := "31337:contract-a:registrant-a"

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	m := fixture(t)
	if err := store.Save(ctx, m); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		if err := store.EnqueueRegistration(ctx, m.FileID, target); err != nil {
			t.Fatal(err)
		}
	}

	job, err := store.ClaimRegistration(ctx, target, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	hash := "0x" + strings.Repeat("a", 64)
	if err := store.SaveRegistrationTransaction(ctx, job, hash); err != nil {
		t.Fatal(err)
	}

	if _, err := store.ClaimRegistration(
		ctx, target, time.Minute,
	); !errors.Is(err, ErrNoRegistrationJob) {
		t.Fatalf("active lease was claimable: %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	// Simulate expiry of a crashed worker's lease.
	if _, err := reopened.db.ExecContext(ctx,
		"UPDATE registration_jobs SET lease_until = 0",
	); err != nil {
		t.Fatal(err)
	}

	recovered, err := reopened.ClaimRegistration(ctx, target, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.TransactionHash != hash || recovered.Attempts != 2 {
		t.Fatalf("transaction recovery failed: %+v", recovered)
	}

	if err := reopened.CompleteRegistration(ctx, job); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale worker retained ownership: %v", err)
	}

	if err := reopened.CompleteRegistration(ctx, recovered); err != nil {
		t.Fatal(err)
	}

	// Enqueueing again must preserve the completed state.
	if err := reopened.EnqueueRegistration(ctx, m.FileID, target); err != nil {
		t.Fatal(err)
	}

	status, err := reopened.GetRegistrationStatus(ctx, m.FileID, target)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != "registered" || status.TransactionHash != hash {
		t.Fatalf("completed registration changed: %+v", status)
	}

	if _, err := reopened.ClaimRegistration(
		ctx, target, time.Minute,
	); !errors.Is(err, ErrNoRegistrationJob) {
		t.Fatalf("completed registration was claimable: %v", err)
	}

	// A different destination needs its own registration.
	otherTarget := "31337:contract-b:registrant-a"
	if err := reopened.EnqueueRegistration(ctx, m.FileID, otherTarget); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.ClaimRegistration(ctx, otherTarget, time.Minute); err != nil {
		t.Fatal(err)
	}
}
