package registry

import (
	"encoding/binary"
	"testing"
)

func TestDecodeRecord(t *testing.T) {
	data := make([]byte, 96)
	data[0] = 1
	binary.BigEndian.PutUint64(data[56:64], 123)
	binary.BigEndian.PutUint64(data[88:96], 1000)

	record, err := decodeRecord(data)
	if err != nil {
		t.Fatal(err)
	}
	if record.Commitment[0] != 1 ||
		record.FileSize != 123 ||
		record.RegisteredAt != 1000 {
		t.Fatal("incorrect record decoding")
	}

	if _, err := decodeRecord(data[:95]); err == nil {
		t.Fatal("accepted truncated response")
	}

	data[32] = 1
	if _, err := decodeRecord(data); err == nil {
		t.Fatal("accepted noncanonical uint64 encoding")
	}
}
