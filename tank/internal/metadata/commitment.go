package metadata

import (
	"bytes"
	"encoding/binary"

	"tank.local/tank/internal/encoding"
	"tank.local/tank/internal/integrity"
)

// Commitment binds immutable file data using a versioned binary format.
//
// Format:
//
//	domain: "TANK_FILE_COMMITMENT_V1" followed by one zero byte
//	file ID: 32 bytes
//	original file size: uint64 big-endian
//	data shard count: uint16 big-endian
//	parity shard count: uint16 big-endian
//	segment count: uint64 big-endian
//	each segment:
//	  index: uint64 big-endian
//	  original segment size: uint64 big-endian
//	  Merkle root: 32 bytes
//
// The commitment is SHA-256 of those bytes.
// Timestamps and node assignments are intentionally excluded.
func (m Manifest) Commitment() (integrity.Hash, error) {
	if err := m.Validate(); err != nil {
		return integrity.Hash{}, err
	}

	var buffer bytes.Buffer
	buffer.WriteString("TANK_FILE_COMMITMENT_V1")
	buffer.WriteByte(0)

	fileID, err := integrity.ParseHash(m.FileID)
	if err != nil {
		return integrity.Hash{}, err
	}
	buffer.Write(fileID[:])

	write64 := func(value uint64) {
		var encoded [8]byte
		binary.BigEndian.PutUint64(encoded[:], value)
		buffer.Write(encoded[:])
	}

	write16 := func(value uint16) {
		var encoded [2]byte
		binary.BigEndian.PutUint16(encoded[:], value)
		buffer.Write(encoded[:])
	}

	write64(uint64(m.Size))
	write16(uint16(encoding.DataShards))
	write16(uint16(encoding.ParityShards))
	write64(uint64(len(m.Segments)))

	for _, segment := range m.Segments {
		write64(uint64(segment.Index))
		write64(uint64(segment.Size))

		root, err := integrity.ParseHash(segment.MerkleRoot)
		if err != nil {
			return integrity.Hash{}, err
		}
		buffer.Write(root[:])
	}

	return integrity.Digest(buffer.Bytes()), nil
}
