package metadata

import (
	"fmt"
	"time"

	"tank.local/tank/internal/encoding"
	"tank.local/tank/internal/integrity"
)

type Shard struct {
	Index   int    `json:"index"`
	Hash    string `json:"hash"`
	NodeURL string `json:"node_url"`
}

type Segment struct {
	Index      int     `json:"index"`
	Size       int     `json:"size"`
	MerkleRoot string  `json:"merkle_root"`
	Shards     []Shard `json:"shards"`
}

type Manifest struct {
	Version   int       `json:"version"`
	FileID    string    `json:"file_id"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
	Segments  []Segment `json:"segments"`
}

func validHash(value string) bool {
	h, err := integrity.ParseHash(value)
	return err == nil && h.String() == value
}

// Validate checks structure. It cannot verify shard contents without their bytes.
func (m Manifest) Validate() error {
	if m.Version != 1 {
		return fmt.Errorf("unsupported manifest version: %d", m.Version)
	}
	if !validHash(m.FileID) {
		return fmt.Errorf("invalid file ID")
	}
	if m.Size < 1 || len(m.Segments) == 0 {
		return fmt.Errorf("file must contain at least one non-empty segment")
	}

	var total int64

	for i, segment := range m.Segments {
		if segment.Index != i {
			return fmt.Errorf("segment indices must be ordered and contiguous")
		}
		if segment.Size < 1 || segment.Size > encoding.MaxBytes {
			return fmt.Errorf("segment %d has an invalid size", i)
		}
		if !validHash(segment.MerkleRoot) {
			return fmt.Errorf("segment %d has an invalid Merkle root", i)
		}
		if len(segment.Shards) != encoding.TotalShards {
			return fmt.Errorf("segment %d must contain six shard records", i)
		}

		for j, shard := range segment.Shards {
			if shard.Index != j || !validHash(shard.Hash) {
				return fmt.Errorf("segment %d shard %d is invalid", i, j)
			}
		}

		// Prevent overflow and reject lengths larger than the stated file size.
		if int64(segment.Size) > m.Size-total {
			return fmt.Errorf("segment sizes exceed file size")
		}
		total += int64(segment.Size)
	}

	if total != m.Size {
		return fmt.Errorf("segment sizes do not match file size")
	}

	return nil
}
