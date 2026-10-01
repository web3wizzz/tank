package metadata

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestRepairQueueSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tank.sqlite")

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	m := fixture(t)
	if err := store.Save(ctx, m); err != nil {
		t.Fatal(err)
	}

	// Repeated enqueue calls should create only one job.
	for i := 0; i < 2; i++ {
		if err := store.EnqueueRepair(ctx, m.FileID); err != nil {
			t.Fatal(err)
		}
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	job, err := reopened.ClaimRepair(ctx, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if job.FileID != m.FileID || job.Attempts != 1 {
		t.Fatalf("unexpected job: %+v", job)
	}

	// Another worker must not claim an actively leased job.
	if _, err := reopened.ClaimRepair(ctx, time.Minute); !errors.Is(err, ErrNoRepairJob) {
		t.Fatalf("expected no claimable job, got %v", err)
	}

	// A worker with the wrong token must not finish someone else's job.
	wrong := job
	wrong.Token = "wrong-token"
	if err := reopened.CompleteRepair(ctx, wrong); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("expected lease rejection, got %v", err)
	}

	if err := reopened.RetryRepair(ctx, job, time.Minute, "node unavailable"); err != nil {
		t.Fatal(err)
	}

	if _, err := reopened.ClaimRepair(ctx, time.Minute); !errors.Is(err, ErrNoRepairJob) {
		t.Fatalf("retry was available too early: %v", err)
	}

	// Advance the stored schedule without sleeping.
	if _, err := reopened.db.ExecContext(ctx,
		"UPDATE repair_jobs SET next_attempt = 0",
	); err != nil {
		t.Fatal(err)
	}

	second, err := reopened.ClaimRepair(ctx, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if second.Attempts != 2 || second.Token == job.Token {
		t.Fatal("retry did not receive a fresh lease")
	}

	// Simulate a crashed worker whose lease has expired.
	if _, err := reopened.db.ExecContext(ctx,
		"UPDATE repair_jobs SET lease_until = 0",
	); err != nil {
		t.Fatal(err)
	}

	third, err := reopened.ClaimRepair(ctx, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	if err := reopened.CompleteRepair(ctx, second); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale worker completed reclaimed job: %v", err)
	}

	if err := reopened.CompleteRepair(ctx, third); err != nil {
		t.Fatal(err)
	}

	if _, err := reopened.ClaimRepair(ctx, time.Minute); !errors.Is(err, ErrNoRepairJob) {
		t.Fatalf("completed job still exists: %v", err)
	}
}
