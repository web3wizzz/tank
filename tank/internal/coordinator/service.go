package coordinator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"tank.local/tank/internal/encoding"
	"tank.local/tank/internal/integrity"
	"tank.local/tank/internal/metadata"
	"tank.local/tank/internal/node"
	"tank.local/tank/internal/storage"
)

const MaxFileBytes = 16 << 20

var (
	ErrInvalidFile = errors.New("file must contain 1 byte to 16 MiB")
	ErrUnavailable = errors.New("not enough verified shards available")
)

type Service struct {
	store       *metadata.Store
	nodes       []*node.Client
	byURL       map[string]*node.Client
	segmentSize int
}

func NewService(
	store *metadata.Store,
	nodes []*node.Client,
	segmentSize int,
) (*Service, error) {
	if store == nil || len(nodes) < 3 {
		return nil, fmt.Errorf("metadata store and three nodes are required")
	}
	if segmentSize < 1 || segmentSize > encoding.MaxBytes {
		return nil, fmt.Errorf("invalid segment size")
	}

	byURL := make(map[string]*node.Client)
	for _, client := range nodes {
		if client == nil {
			return nil, fmt.Errorf("nil node client")
		}
		if _, exists := byURL[client.BaseURL]; exists {
			return nil, fmt.Errorf("node URLs must be distinct")
		}
		byURL[client.BaseURL] = client
	}

	return &Service{
		store:       store,
		nodes:       append([]*node.Client(nil), nodes[:3]...),
		byURL:       byURL,
		segmentSize: segmentSize,
	}, nil
}

func (s *Service) Tank(ctx context.Context, data []byte) (metadata.Manifest, error) {
	var empty metadata.Manifest
	if len(data) == 0 || len(data) > MaxFileBytes {
		return empty, ErrInvalidFile
	}

	id := integrity.Digest(data).String()

	// Repeated tanking returns the existing manifest.
	existing, err := s.store.Load(ctx, id)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, metadata.ErrNotFound) {
		return empty, err
	}

	m := metadata.Manifest{
		Version:   1,
		FileID:    id,
		Size:      int64(len(data)),
		CreatedAt: time.Now().UTC(),
	}

	for offset := 0; offset < len(data); offset += s.segmentSize {
		if err := ctx.Err(); err != nil {
			return empty, err
		}

		end := min(offset+s.segmentSize, len(data))
		part := data[offset:end]

		shards, err := encoding.Encode(part)
		if err != nil {
			return empty, err
		}

		root, _, err := integrity.BuildMerkleRoot(shards)
		if err != nil {
			return empty, err
		}

		segment := metadata.Segment{
			Index:      len(m.Segments),
			Size:       len(part),
			MerkleRoot: root.String(),
			Shards:     make([]metadata.Shard, len(shards)),
		}

		for index, shard := range shards {
			client := s.nodes[index%len(s.nodes)]
			hash := integrity.Digest(shard)
			key := storage.ShardKey{
				FileID: id, Segment: segment.Index, Index: index,
			}

			if err := client.Put(ctx, key, shard, hash); err != nil {
				return empty, fmt.Errorf(
					"store segment %d shard %d: %w",
					segment.Index, index, err,
				)
			}

			segment.Shards[index] = metadata.Shard{
				Index: index, Hash: hash.String(), NodeURL: client.BaseURL,
			}
		}

		m.Segments = append(m.Segments, segment)
	}

	// Publish the manifest only after every shard has been accepted.
	if err := s.store.Save(ctx, m); err != nil {
		if errors.Is(err, metadata.ErrExists) {
			return s.store.Load(ctx, id)
		}
		return empty, err
	}

	return m, nil
}

func (s *Service) Retrieve(ctx context.Context, id string) ([]byte, error) {
	m, err := s.store.Load(ctx, id)
	if err != nil {
		return nil, err
	}
	if m.Size > MaxFileBytes {
		return nil, fmt.Errorf("file exceeds this coordinator's size limit")
	}

	var output bytes.Buffer

	for _, segment := range m.Segments {
		shards := make([][]byte, encoding.TotalShards)
		expectedSize := (segment.Size + encoding.DataShards - 1) /
			encoding.DataShards

		for _, record := range segment.Shards {
			if err := ctx.Err(); err != nil {
				return nil, err
			}

			// Only contact explicitly configured nodes.
			client, configured := s.byURL[record.NodeURL]
			if !configured {
				continue
			}

			expected, err := integrity.ParseHash(record.Hash)
			if err != nil {
				return nil, err
			}

			key := storage.ShardKey{
				FileID: id, Segment: segment.Index, Index: record.Index,
			}

			data, err := client.Get(ctx, key, expected)
			if err != nil || len(data) != expectedSize {
				continue
			}
			shards[record.Index] = data
		}

		if err := ctx.Err(); err != nil {
			return nil, err
		}

		complete, err := encoding.Reconstruct(shards, segment.Size)
		if err != nil {
			return nil, fmt.Errorf(
				"%w: segment %d: %v",
				ErrUnavailable, segment.Index, err,
			)
		}

		// Verify reconstructed shards, including those missing from nodes.
		for i, shard := range complete {
			if integrity.Digest(shard).String() != segment.Shards[i].Hash {
				return nil, fmt.Errorf("reconstructed shard hash mismatch")
			}
		}

		root, _, err := integrity.BuildMerkleRoot(complete)
		if err != nil {
			return nil, err
		}
		if root.String() != segment.MerkleRoot {
			return nil, fmt.Errorf("segment Merkle root mismatch")
		}

		remaining := segment.Size
		for _, shard := range complete[:encoding.DataShards] {
			n := min(remaining, len(shard))
			output.Write(shard[:n])
			remaining -= n
		}
	}

	if int64(output.Len()) != m.Size ||
		integrity.Digest(output.Bytes()).String() != m.FileID {
		return nil, fmt.Errorf("recovered file failed integrity verification")
	}

	return output.Bytes(), nil
}

func (s *Service) List(ctx context.Context, after string) ([]string, error) {
	return s.store.List(ctx, after, 100)
}
