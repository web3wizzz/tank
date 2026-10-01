package coordinator

import (
	"context"
	"encoding/json"
	"fmt"

	"tank.local/tank/internal/encoding"
	"tank.local/tank/internal/integrity"
	"tank.local/tank/internal/metadata"
	"tank.local/tank/internal/storage"
)

// Repair reconstructs unavailable shards, verifies replacement storage,
// and then updates their locations using a conditional database write.
func (s *Service) Repair(
	ctx context.Context,
	id string,
	replacementURL string,
) (int, error) {
	target, configured := s.byURL[replacementURL]
	if !configured {
		return 0, fmt.Errorf("replacement node is not configured")
	}

	original, err := s.store.Load(ctx, id)
	if err != nil {
		return 0, err
	}
	if original.Size > MaxFileBytes {
		return 0, fmt.Errorf("file exceeds this coordinator's size limit")
	}

	// Deep-copy nested slices so original remains unchanged for the
	// conditional metadata update.
	body, err := json.Marshal(original)
	if err != nil {
		return 0, err
	}
	var updated metadata.Manifest
	if err := json.Unmarshal(body, &updated); err != nil {
		return 0, err
	}

	repaired := 0

	for segmentIndex, segment := range original.Segments {
		shards := make([][]byte, encoding.TotalShards)
		expectedSize := (segment.Size + encoding.DataShards - 1) /
			encoding.DataShards

		for _, record := range segment.Shards {
			if err := ctx.Err(); err != nil {
				return 0, err
			}

			client, exists := s.byURL[record.NodeURL]
			if !exists {
				continue
			}

			hash, err := integrity.ParseHash(record.Hash)
			if err != nil {
				return 0, err
			}

			key := storage.ShardKey{
				FileID: id, Segment: segment.Index, Index: record.Index,
			}

			data, err := client.Get(ctx, key, hash)
			if err == nil && len(data) == expectedSize {
				shards[record.Index] = data
			}
		}

		if err := ctx.Err(); err != nil {
			return 0, err
		}

		var missing []int
		for index, shard := range shards {
			if shard == nil {
				missing = append(missing, index)
			}
		}
		if len(missing) == 0 {
			continue
		}

		// Preserve the initial placement model: at most two shards
		// of a segment on any one node.
		if len(missing) > encoding.ParityShards {
			return 0, fmt.Errorf(
				"%w: segment %d has %d unavailable shards",
				ErrUnavailable, segment.Index, len(missing),
			)
		}
		for _, record := range segment.Shards {
			if record.NodeURL == replacementURL {
				return 0, fmt.Errorf(
					"replacement already holds shards for segment %d; choose another node",
					segment.Index,
				)
			}
		}

		complete, err := encoding.Reconstruct(shards, segment.Size)
		if err != nil {
			return 0, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}

		for index, shard := range complete {
			if integrity.Digest(shard).String() != segment.Shards[index].Hash {
				return 0, fmt.Errorf("reconstructed shard hash mismatch")
			}
		}

		root, _, err := integrity.BuildMerkleRoot(complete)
		if err != nil {
			return 0, err
		}
		if root.String() != segment.MerkleRoot {
			return 0, fmt.Errorf("reconstructed segment root mismatch")
		}

		for _, index := range missing {
			key := storage.ShardKey{
				FileID: id, Segment: segment.Index, Index: index,
			}
			hash := integrity.Digest(complete[index])

			if err := target.Put(ctx, key, complete[index], hash); err != nil {
				return 0, fmt.Errorf("replacement write failed: %w", err)
			}

			// Read back and verify before recording the new location.
			if _, err := target.Get(ctx, key, hash); err != nil {
				return 0, fmt.Errorf("replacement verification failed: %w", err)
			}

			updated.Segments[segmentIndex].Shards[index].NodeURL = replacementURL
			repaired++
		}
	}

	if repaired == 0 {
		return 0, nil
	}

	if err := s.store.ReplaceLocations(ctx, original, updated); err != nil {
		return 0, err
	}

	return repaired, nil
}
