package integrity

import (
	"encoding/binary"
	"fmt"
)

// Proof identifies one shard and its path through the tree.
// Index and leaf count are authenticated by the commitment.
type Proof struct {
	Index     int    `json:"index"`
	LeafCount int    `json:"leaf_count"`
	Siblings  []Hash `json:"siblings"`
}

// Separate hash domains for leaves, branches, and the final commitment.
func leafHash(index int, shard []byte) Hash {
	data := make([]byte, 9+len(shard))
	data[0] = 0
	binary.BigEndian.PutUint64(data[1:9], uint64(index))
	copy(data[9:], shard)
	return Digest(data)
}

func branchHash(left, right Hash) Hash {
	var data [65]byte
	data[0] = 1
	copy(data[1:33], left[:])
	copy(data[33:], right[:])
	return Digest(data[:])
}

func commitRoot(count int, top Hash) Hash {
	var data [41]byte
	data[0] = 2
	binary.BigEndian.PutUint64(data[1:9], uint64(count))
	copy(data[9:], top[:])
	return Digest(data[:])
}

// BuildMerkleRoot commits to ordered shards and returns one proof per shard.
// For odd-width levels, the final node is paired with itself.
func BuildMerkleRoot(shards [][]byte) (Hash, []Proof, error) {
	if len(shards) == 0 {
		return Hash{}, nil, fmt.Errorf("cannot build an empty Merkle tree")
	}

	leaves := make([]Hash, len(shards))
	for i, shard := range shards {
		leaves[i] = leafHash(i, shard)
	}

	levels := [][]Hash{leaves}

	for len(levels[len(levels)-1]) > 1 {
		current := levels[len(levels)-1]
		next := make([]Hash, len(current)/2+len(current)%2)

		for i := range next {
			left := current[2*i]
			right := left

			if 2*i+1 < len(current) {
				right = current[2*i+1]
			}

			next[i] = branchHash(left, right)
		}

		levels = append(levels, next)
	}

	proofs := make([]Proof, len(shards))

	for i := range shards {
		proof := Proof{
			Index:     i,
			LeafCount: len(shards),
			Siblings:  make([]Hash, 0, len(levels)-1),
		}

		position := i

		for level := 0; level < len(levels)-1; level++ {
			nodes := levels[level]
			sibling := position ^ 1

			if sibling >= len(nodes) {
				sibling = position
			}

			proof.Siblings = append(proof.Siblings, nodes[sibling])
			position /= 2
		}

		proofs[i] = proof
	}

	top := levels[len(levels)-1][0]
	return commitRoot(len(shards), top), proofs, nil
}

// VerifyProof checks a shard against a trusted commitment.
// The caller must obtain the root from an authenticated manifest or registry.
func VerifyProof(root Hash, shard []byte, proof Proof) bool {
	if proof.LeafCount < 1 ||
		proof.Index < 0 ||
		proof.Index >= proof.LeafCount {
		return false
	}

	current := leafHash(proof.Index, shard)
	position := proof.Index
	width := proof.LeafCount
	step := 0

	for width > 1 {
		if step >= len(proof.Siblings) {
			return false
		}

		sibling := proof.Siblings[step]

		// The unpaired final node must use itself as its sibling.
		if width%2 == 1 && position == width-1 && sibling != current {
			return false
		}

		if position%2 == 0 {
			current = branchHash(current, sibling)
		} else {
			current = branchHash(sibling, current)
		}

		position /= 2
		width = width/2 + width%2
		step++
	}

	return step == len(proof.Siblings) &&
		commitRoot(proof.LeafCount, current) == root
}
