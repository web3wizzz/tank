package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"

	"tank.local/tank/internal/encoding"
	"tank.local/tank/internal/integrity"
)

const MaxShardBytes = (encoding.MaxBytes + encoding.DataShards - 1) /
	encoding.DataShards

type Filesystem struct {
	root *os.Root
}

var _ Backend = (*Filesystem)(nil)

func NewFilesystem(directory string) (*Filesystem, error) {
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

	return &Filesystem{root: root}, nil
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

	err = f.root.Remove(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
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
