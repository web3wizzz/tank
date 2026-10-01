package integrity

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

type Hash [32]byte

func Digest(data []byte) Hash {
	return sha256.Sum256(data)
}

func (h Hash) String() string {
	return hex.EncodeToString(h[:])
}

func ParseHash(value string) (Hash, error) {
	var result Hash

	data, err := hex.DecodeString(value)
	if err != nil || len(data) != len(result) {
		return result, fmt.Errorf("expected a 64-character hexadecimal hash")
	}

	copy(result[:], data)
	return result, nil
}
