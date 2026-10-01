package storage

import (
	"context"
	"errors"

	"tank.local/tank/internal/integrity"
)

var ErrShardNotFound = errors.New("shard not found")

type ShardKey struct {
	FileID  string
	Segment int
	Index   int
}

type Backend interface {
	Put(
		ctx context.Context,
		key ShardKey,
		data []byte,
		expected integrity.Hash,
	) error

	Get(ctx context.Context, key ShardKey) ([]byte, error)
	Delete(ctx context.Context, key ShardKey) error
	Close() error
}
