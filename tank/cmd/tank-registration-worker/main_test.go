package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"tank.local/tank/internal/encoding"
	"tank.local/tank/internal/integrity"
	"tank.local/tank/internal/metadata"
	"tank.local/tank/internal/registrationguard"
)

func workerFixture(t *testing.T) (string, string) {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "authority.sqlite")
	ctx := context.Background()
	store, err := metadata.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	data := []byte("synthetic queued ciphertext")
	shards, err := encoding.Encode(data)
	if err != nil {
		t.Fatal(err)
	}
	root, _, err := integrity.BuildMerkleRoot(shards)
	if err != nil {
		t.Fatal(err)
	}
	segment := metadata.Segment{Size: len(data), MerkleRoot: root.String()}
	for index, shard := range shards {
		segment.Shards = append(segment.Shards, metadata.Shard{Index: index, Hash: integrity.Digest(shard).String()})
	}
	id := integrity.Digest(data).String()
	if err := store.Save(ctx, metadata.Manifest{Version: 1, FileID: id, Size: int64(len(data)), CreatedAt: time.Now().UTC(), Segments: []metadata.Segment{segment}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TANK_DATABASE_PATH", path)
	// No listener exists here: enqueue-only must perform no RPC.
	t.Setenv("TANK_CHAIN_RPC", "http://127.0.0.1:1")
	t.Setenv("TANK_REGISTRY_ADDR", "0x1111111111111111111111111111111111111111")
	t.Setenv("TANK_REGISTRANT", "0x2222222222222222222222222222222222222222")
	return path, id
}

func TestEnqueueOnlyPersistsUnattemptedJobAcrossReopen(t *testing.T) {
	path, id := workerFixture(t)
	for range 2 {
		if err := runArgs([]string{"--enqueue-only"}); err != nil {
			t.Fatal(err)
		}
	}
	store, err := metadata.OpenExisting(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	status, err := store.GetRegistrationStatus(context.Background(), id, "31337:0x1111111111111111111111111111111111111111:0x2222222222222222222222222222222222222222")
	if err != nil || status.State != "pending" || status.Attempts != 0 || status.TransactionHash != "" {
		t.Fatalf("queue was attempted or lost across reopen: %+v, %v", status, err)
	}
}

func TestLocalWorkerRefusesRestoreCandidateAndConcurrentLane(t *testing.T) {
	path, _ := workerFixture(t)
	gate := path + ".restore-gate"
	if err := os.WriteFile(gate, []byte("synthetic inactive candidate"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{nil, {"--enqueue-only"}} {
		if err := runArgs(args); !errors.Is(err, registrationguard.ErrBlocked) {
			t.Fatalf("candidate gate ignored: %v", err)
		}
	}
	if err := os.Remove(gate); err != nil { // Only this test's synthetic gate.
		t.Fatal(err)
	}
	guard, err := registrationguard.Acquire(path + ".local-registration.lock")
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	if err := runArgs([]string{"--enqueue-only"}); !errors.Is(err, registrationguard.ErrBlocked) {
		t.Fatalf("concurrent lane accepted: %v", err)
	}
}

func TestEnqueueOnlyRejectsPublicRPCAndUnknownArguments(t *testing.T) {
	workerFixture(t)
	t.Setenv("TANK_CHAIN_RPC", "https://public.invalid")
	if err := runArgs([]string{"--enqueue-only"}); err == nil {
		t.Fatal("public RPC accepted by local worker")
	}
	if err := runArgs([]string{"--broadcast"}); err == nil {
		t.Fatal("unknown argument accepted")
	}
}
