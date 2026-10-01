package encoding

import (
	"bytes"
	"testing"
)

func TestRecoverEveryPair(t *testing.T) {
	for _, size := range []int{1, 3, 4, 1001, 65537} {
		data := make([]byte, size)
		for i := range data {
			data[i] = byte(i * 17)
		}
		original, err := Encode(data)
		if err != nil {
			t.Fatal(err)
		}
		for a := 0; a < TotalShards; a++ {
			for b := a + 1; b < TotalShards; b++ {
				shards := append([][]byte(nil), original...)
				shards[a], shards[b] = nil, nil
				got, err := Decode(shards, len(data))
				if err != nil || !bytes.Equal(got, data) {
					t.Fatalf("size=%d missing=%d,%d: %v", size, a, b, err)
				}
				if shards[a] != nil || shards[b] != nil {
					t.Fatal("Decode mutated caller's shard positions")
				}
			}
		}
	}
}

func TestRejectInvalidInputs(t *testing.T) {
	if _, err := Encode(nil); err == nil {
		t.Fatal("accepted empty segment")
	}
	if _, err := Encode(make([]byte, MaxBytes+1)); err == nil {
		t.Fatal("accepted oversized segment")
	}
	data := []byte("Tank storage test")
	shards, err := Encode(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(shards[:4], len(data)); err == nil {
		t.Fatal("accepted compacted shard positions")
	}
	shards[0], shards[1], shards[2] = nil, nil, nil
	if _, err := Decode(shards, len(data)); err == nil {
		t.Fatal("accepted fewer than four shards")
	}
}

func TestRejectInconsistentShards(t *testing.T) {
	data := []byte("Tank storage test")
	shards, err := Encode(data)
	if err != nil {
		t.Fatal(err)
	}
	shards[0][0] ^= 1
	if _, err := Decode(shards, len(data)); err == nil {
		t.Fatal("accepted inconsistent shards")
	}
}
