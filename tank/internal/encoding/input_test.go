package encoding

import (
	"bytes"
	"testing"
)

func TestEncodePreservesInputAndOwnsShards(t *testing.T) {
	for _, size := range []int{1, 3, 1024, 1025} {
		backing := make([]byte, size*3+100)
		for i := range backing {
			backing[i] = byte(i*17 + 3)
		}

		before := bytes.Clone(backing)
		input := backing[:size]

		shards, err := Encode(input)
		if err != nil {
			t.Fatal(err)
		}

		if !bytes.Equal(backing, before) {
			t.Fatalf("size %d: Encode modified caller buffer", size)
		}

		// Changing the original buffer must not change encoded shards.
		for i := range input {
			input[i] ^= 1
		}

		// Simulate losing one node's two shards.
		shards[0], shards[3] = nil, nil

		got, err := Decode(shards, size)
		if err != nil || !bytes.Equal(got, before[:size]) {
			t.Fatalf(
				"size %d: recovery depends on caller buffer: %v",
				size, err,
			)
		}
	}
}
