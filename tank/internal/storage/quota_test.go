package storage

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"tank.local/tank/internal/integrity"
)

func TestNodeQuotaPreservesExistingDataAndRepairsAtCapacity(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	backend, err := NewFilesystemWithQuota(directory, 4)
	if err != nil {
		t.Fatal(err)
	}
	key := ShardKey{FileID: integrity.Digest([]byte("file")).String(), Index: 0}
	data := []byte("data")
	if err := backend.Put(ctx, key, data, integrity.Digest(data)); err != nil {
		t.Fatal(err)
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	backend, err = NewFilesystemWithQuota(directory, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { backend.Close() }()
	if err := backend.Put(ctx, key, []byte("fix!"), integrity.Digest([]byte("fix!"))); err != nil {
		t.Fatal("same-size repair at quota was refused")
	}
	other := key
	other.Index = 1
	if err := backend.Put(ctx, other, []byte("x"), integrity.Digest([]byte("x"))); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatal("node quota accepted new shard")
	}
	got, err := backend.Get(ctx, key)
	if err != nil || !bytes.Equal(got, []byte("fix!")) {
		t.Fatal("quota failure changed the existing shard")
	}
	if err := backend.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err := backend.Put(ctx, other, data, integrity.Digest(data)); err != nil {
		t.Fatal("deleting an owned test shard did not restore capacity")
	}
	// A lower configured quota still permits reading retained data; it never deletes it.
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	backend, err = NewFilesystemWithQuota(directory, 1)
	if err != nil {
		t.Fatal(err)
	}
	got, err = backend.Get(ctx, other)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("lower quota hid existing data")
	}
}
func TestConcurrentNodeWritesCannotOversubscribeQuota(t *testing.T) {
	backend, err := NewFilesystemWithQuota(t.TempDir(), 4)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { backend.Close() }()
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for index := 0; index < 8; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			key := ShardKey{FileID: integrity.Digest([]byte{byte(index)}).String()}
			results <- backend.Put(context.Background(), key, []byte("data"), integrity.Digest([]byte("data")))
		}(index)
	}
	wg.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		} else if !errors.Is(err, ErrQuotaExceeded) {
			t.Fatal(err)
		}
	}
	if accepted != 1 {
		t.Fatal("concurrent node writes exceeded quota")
	}
}
func TestExistingUnmanagedFilesCountAgainstNodeQuota(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "retained-data"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	backend, err := NewFilesystemWithQuota(directory, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { backend.Close() }()
	key := ShardKey{FileID: integrity.Digest([]byte("file")).String()}
	if err := backend.Put(context.Background(), key, []byte("x"), integrity.Digest([]byte("x"))); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatal("existing files were omitted from capacity accounting")
	}
	if data, err := os.ReadFile(filepath.Join(directory, "retained-data")); err != nil || string(data) != "data" {
		t.Fatal("capacity initialization changed existing data")
	}
}

func TestNodeFileCountLimitSurvivesRestartAndAllowsReplacement(t *testing.T) {
	directory := t.TempDir()
	backend, err := NewFilesystemWithQuota(directory, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	key := ShardKey{FileID: integrity.Digest([]byte("file")).String()}
	data := []byte("x")
	if err := backend.Put(context.Background(), key, data, integrity.Digest(data)); err != nil {
		t.Fatal(err)
	}
	backend.Close()
	backend, err = NewFilesystemWithQuota(directory, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	if err := backend.Put(context.Background(), key, data, integrity.Digest(data)); err != nil {
		t.Fatal("file-count quota prevented replacement")
	}
	key.Index = 1
	if err := backend.Put(context.Background(), key, data, integrity.Digest(data)); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatal("new tiny shard bypassed file-count quota")
	}
}
