package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

var ErrManifestConflict = errors.New("manifest changed; reload before retrying")

// ReplaceLocations updates node assignments while preserving file commitments.
// The update succeeds only if the stored manifest still matches original.
func (s *Store) ReplaceLocations(
	ctx context.Context,
	original Manifest,
	updated Manifest,
) error {
	if err := original.Validate(); err != nil {
		return err
	}
	if err := updated.Validate(); err != nil {
		return err
	}

	if original.Version != updated.Version ||
		original.FileID != updated.FileID ||
		original.Size != updated.Size ||
		!original.CreatedAt.Equal(updated.CreatedAt) ||
		len(original.Segments) != len(updated.Segments) {
		return fmt.Errorf("repair cannot change file identity or structure")
	}

	for i, before := range original.Segments {
		after := updated.Segments[i]

		if before.Index != after.Index ||
			before.Size != after.Size ||
			before.MerkleRoot != after.MerkleRoot ||
			len(before.Shards) != len(after.Shards) {
			return fmt.Errorf("repair cannot change segment commitments")
		}

		for j, shard := range before.Shards {
			replacement := after.Shards[j]
			if shard.Index != replacement.Index ||
				shard.Hash != replacement.Hash {
				return fmt.Errorf("repair cannot change shard commitments")
			}
			if replacement.NodeURL == "" {
				return fmt.Errorf("shard location cannot be empty")
			}
		}
	}

	beforeJSON, err := json.Marshal(original)
	if err != nil {
		return err
	}
	afterJSON, err := json.Marshal(updated)
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE manifests
		SET body = ?
		WHERE file_id = ? AND body = ?
	`, string(afterJSON), original.FileID, string(beforeJSON))
	if err != nil {
		return err
	}

	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrManifestConflict
	}

	return nil
}
