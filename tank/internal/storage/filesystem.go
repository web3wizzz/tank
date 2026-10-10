package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"tank.local/tank/internal/encoding"
	"tank.local/tank/internal/integrity"
)

const MaxShardBytes = (encoding.MaxBytes + encoding.DataShards - 1) /
	encoding.DataShards

type Filesystem struct {
	mu         sync.Mutex
	quotaBytes int64
	quotaFiles int
	usedFiles  int
	usedBytes  int64
	root       *os.Root
}

var _ Backend = (*Filesystem)(nil)

var ErrQuotaExceeded = errors.New("node storage quota exceeded")

func NewFilesystem(directory string) (*Filesystem, error) {
	return NewFilesystemWithQuota(directory, 0)
}

// NewFilesystemWithQuota counts existing flat-directory files without deleting
// anything. A zero quota disables capacity enforcement. Use one node per directory.
func NewFilesystemWithQuota(directory string, quotaBytes int64, fileLimits ...int) (*Filesystem, error) {
	var quotaFiles int
	if len(fileLimits) > 1 {
		return nil, fmt.Errorf("expected at most one file-count limit")
	}
	if len(fileLimits) == 1 {
		quotaFiles = fileLimits[0]
	}
	if quotaFiles < 0 || quotaFiles > 1_000_000_000 {
		return nil, fmt.Errorf("invalid node file-count limit")
	}
	if quotaBytes < 0 || quotaBytes > 1<<50 {
		return nil, fmt.Errorf("invalid node storage quota")
	}

	if directory == "" {
		return nil, fmt.Errorf("storage directory is required")
	}

	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}

	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}

	f := &Filesystem{root: root, quotaBytes: quotaBytes, quotaFiles: quotaFiles}
	entries, err := root.Open(".")
	if err != nil {
		root.Close()
		return nil, err
	}
	files, err := entries.ReadDir(-1)
	entries.Close()
	if err != nil {
		root.Close()
		return nil, err
	}
	for _, entry := range files {
		info, err := root.Lstat(entry.Name())
		if err != nil {
			root.Close()
			return nil, err
		}
		if info.Mode().IsRegular() {
			if info.Size() > 1<<50-f.usedBytes {
				root.Close()
				return nil, fmt.Errorf("storage directory accounting exceeds supported capacity")
			}
			f.usedBytes += info.Size()
			f.usedFiles++
		}
	}
	return f, nil
}

func (f *Filesystem) Close() error {
	return f.root.Close()
}

func shardFilename(key ShardKey) (string, error) {
	hash, err := integrity.ParseHash(key.FileID)
	if err != nil || hash.String() != key.FileID {
		return "", fmt.Errorf("invalid file ID")
	}

	if key.Segment < 0 {
		return "", fmt.Errorf("segment index cannot be negative")
	}

	if key.Index < 0 || key.Index >= encoding.TotalShards {
		return "", fmt.Errorf("invalid shard index")
	}

	return fmt.Sprintf(
		"%s.%d.%d.shard",
		key.FileID,
		key.Segment,
		key.Index,
	), nil
}

// Put validates the bytes, writes a temporary file, and atomically
// replaces the destination. Replacement permits repair of corrupt shards.
func (f *Filesystem) Put(
	ctx context.Context,
	key ShardKey,
	data []byte,
	expected integrity.Hash,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	name, err := shardFilename(key)
	if err != nil {
		return err
	}

	if len(data) == 0 || len(data) > MaxShardBytes {
		return fmt.Errorf("invalid shard size")
	}

	if integrity.Digest(data) != expected {
		return fmt.Errorf("shard hash mismatch")
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	var previousBytes int64
	replacing := false
	info, err := f.root.Lstat(name)
	if err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("existing shard must be a regular file")
		}
		previousBytes = info.Size()
		replacing = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if !replacing && f.quotaFiles > 0 && f.usedFiles >= f.quotaFiles {
		return ErrQuotaExceeded
	}
	nextBytes := f.usedBytes - previousBytes + int64(len(data))
	if f.quotaBytes > 0 && nextBytes > f.quotaBytes {
		return ErrQuotaExceeded
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}

	temporary := ".pending-" + hex.EncodeToString(random[:])

	file, err := f.root.OpenFile(
		temporary,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		0600,
	)
	if err != nil {
		return err
	}

	defer f.root.Remove(temporary)

	written, err := file.Write(data)
	if err != nil {
		file.Close()
		return err
	}
	if written != len(data) {
		file.Close()
		return io.ErrShortWrite
	}

	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := f.root.Rename(temporary, name); err != nil {
		return err
	}

	f.usedBytes = nextBytes
	if !replacing {
		f.usedFiles++
	}
	return f.syncDirectory()
}

// Get returns bounded shard bytes.
// The retrieval service must verify them against the manifest hash.
func (f *Filesystem) Get(
	ctx context.Context,
	key ShardKey,
) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	name, err := shardFilename(key)
	if err != nil {
		return nil, err
	}

	info, err := f.root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrShardNotFound
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("shard must be a regular file")
	}

	file, err := f.root.Open(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrShardNotFound
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, MaxShardBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > MaxShardBytes {
		return nil, fmt.Errorf("stored shard has an invalid size")
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return data, nil
}

// Delete is idempotent: deleting an absent shard succeeds.
func (f *Filesystem) Delete(
	ctx context.Context,
	key ShardKey,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	name, err := shardFilename(key)
	if err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	var previousBytes int64
	removedRegular := false
	if info, err := f.root.Lstat(name); err == nil && info.Mode().IsRegular() {
		previousBytes = info.Size()
		removedRegular = true
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	err = f.root.Remove(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	f.usedBytes -= previousBytes
	if removedRegular {
		f.usedFiles--
	}
	return f.syncDirectory()
}

// Persist directory changes on the Linux filesystem used by Codespaces.
func (f *Filesystem) syncDirectory() error {
	directory, err := f.root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()

	return directory.Sync()
}
