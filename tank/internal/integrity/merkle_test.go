package integrity

import (
	"fmt"
	"testing"
)

func TestProofsForDifferentTreeSizes(t *testing.T) {
	for _, count := range []int{1, 2, 3, 6, 7, 150} {
		shards := make([][]byte, count)

		for i := range shards {
			shards[i] = []byte(fmt.Sprintf("Tank shard %d", i))
		}

		root, proofs, err := BuildMerkleRoot(shards)
		if err != nil {
			t.Fatal(err)
		}

		for i, proof := range proofs {
			if !VerifyProof(root, shards[i], proof) {
				t.Fatalf("count=%d index=%d: valid proof rejected", count, i)
			}

			if VerifyProof(root, []byte("altered shard"), proof) {
				t.Fatal("accepted altered content")
			}
		}
	}
}

func TestRejectAlteredProof(t *testing.T) {
	shards := [][]byte{[]byte("a"), []byte("b"), []byte("c")}
	root, proofs, err := BuildMerkleRoot(shards)
	if err != nil {
		t.Fatal(err)
	}

	proof := proofs[0]
	proof.Index = 1
	if VerifyProof(root, shards[0], proof) {
		t.Fatal("accepted wrong index")
	}

	proof = proofs[0]
	proof.LeafCount = 4
	if VerifyProof(root, shards[0], proof) {
		t.Fatal("accepted wrong leaf count")
	}

	proof = proofs[0]
	proof.Siblings = append([]Hash(nil), proof.Siblings...)
	proof.Siblings[0][0] ^= 1
	if VerifyProof(root, shards[0], proof) {
		t.Fatal("accepted altered sibling")
	}

	proof = proofs[0]
	proof.Siblings = proof.Siblings[:1]
	if VerifyProof(root, shards[0], proof) {
		t.Fatal("accepted incomplete proof")
	}

	proof = proofs[0]
	proof.Siblings = append(append([]Hash(nil), proof.Siblings...), Hash{})
	if VerifyProof(root, shards[0], proof) {
		t.Fatal("accepted extra sibling")
	}

	if VerifyProof(Hash{}, shards[0], proofs[0]) {
		t.Fatal("accepted wrong root")
	}
}

func TestEmptyTreeRejected(t *testing.T) {
	if _, _, err := BuildMerkleRoot(nil); err == nil {
		t.Fatal("accepted empty tree")
	}
}

func TestHashRoundTrip(t *testing.T) {
	original := Digest([]byte("Tank"))

	parsed, err := ParseHash(original.String())
	if err != nil || parsed != original {
		t.Fatal("hash round trip failed")
	}

	if _, err := ParseHash("invalid"); err == nil {
		t.Fatal("accepted invalid hash")
	}
}
