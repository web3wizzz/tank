package encoding

import (
	"bytes"
	"fmt"

	"github.com/klauspost/reedsolomon"
)

const (
	DataShards   = 4
	ParityShards = 2
	TotalShards  = DataShards + ParityShards
	MaxBytes     = 16 << 20
)

// Encode processes one segment. Save its original length in the manifest.
func Encode(data []byte) ([][]byte, error) {
	if len(data) == 0 || len(data) > MaxBytes {
		return nil, fmt.Errorf("segment must contain 1 byte to 16 MiB")
	}
	enc, err := reedsolomon.New(DataShards, ParityShards)
	if err != nil {
		return nil, err
	}
	shards, err := enc.Split(bytes.Clone(data))
	if err != nil {
		return nil, err
	}
	if err := enc.Encode(shards); err != nil {
		return nil, err
	}
	return shards, nil
}

// Reconstruct preserves caller buffers.
// Keep all six positions; use nil for missing or hash-invalid shards.
// Verify shard hashes before calling.
func Reconstruct(shards [][]byte, originalSize int) ([][]byte, error) {
	if originalSize < 1 || originalSize > MaxBytes {
		return nil, fmt.Errorf("invalid original segment size")
	}
	if len(shards) != TotalShards {
		return nil, fmt.Errorf("expected %d indexed shard positions", TotalShards)
	}

	shardSize := (originalSize + DataShards - 1) / DataShards
	owned := make([][]byte, TotalShards)
	available := 0

	for i, shard := range shards {
		if len(shard) == 0 {
			continue
		}
		if len(shard) != shardSize {
			return nil, fmt.Errorf(
				"shard %d: expected %d bytes, got %d",
				i, shardSize, len(shard),
			)
		}
		owned[i] = bytes.Clone(shard)
		available++
	}
	if available < DataShards {
		return nil, fmt.Errorf("need %d valid shards; have %d", DataShards, available)
	}

	enc, err := reedsolomon.New(DataShards, ParityShards)
	if err != nil {
		return nil, err
	}
	if err := enc.Reconstruct(owned); err != nil {
		return nil, err
	}
	valid, err := enc.Verify(owned)
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, fmt.Errorf("shards are not a consistent codeword")
	}
	return owned, nil
}

// Decode removes padding. Verify the recovered content hash afterward.
func Decode(shards [][]byte, originalSize int) ([]byte, error) {
	complete, err := Reconstruct(shards, originalSize)
	if err != nil {
		return nil, err
	}
	enc, err := reedsolomon.New(DataShards, ParityShards)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := enc.Join(&out, complete, originalSize); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
