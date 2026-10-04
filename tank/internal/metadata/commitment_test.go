package metadata

import (
	"encoding/json"
	"testing"
	"time"

	"tank.local/tank/internal/integrity"
)

func TestCommitmentStableAcrossRepair(t *testing.T) {
	m := fixture(t)

	before, err := m.Commitment()
	if err != nil {
		t.Fatal(err)
	}

	// Repair may change locations without changing content.
	for i := range m.Segments {
		for j := range m.Segments[i].Shards {
			m.Segments[i].Shards[j].NodeURL = "http://127.0.0.1:9104"
		}
	}
	m.CreatedAt = m.CreatedAt.Add(time.Hour)

	after, err := m.Commitment()
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("node locations or timestamp changed the commitment")
	}

	// Serializing and loading a manifest must preserve the commitment.
	body, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}

	var loaded Manifest
	if err := json.Unmarshal(body, &loaded); err != nil {
		t.Fatal(err)
	}

	reloaded, err := loaded.Commitment()
	if err != nil || reloaded != before {
		t.Fatalf("commitment changed after JSON round trip: %v", err)
	}
}

func TestCommitmentBindsFileAndSegmentRoots(t *testing.T) {
	m := fixture(t)
	original, err := m.Commitment()
	if err != nil {
		t.Fatal(err)
	}

	m.FileID = integrity.Digest([]byte("different file")).String()
	changedFile, err := m.Commitment()
	if err != nil {
		t.Fatal(err)
	}
	if changedFile == original {
		t.Fatal("file ID was not committed")
	}

	m = fixture(t)
	m.Segments[0].MerkleRoot =
		integrity.Digest([]byte("different segment root")).String()

	changedRoot, err := m.Commitment()
	if err != nil {
		t.Fatal(err)
	}
	if changedRoot == original {
		t.Fatal("segment root was not committed")
	}
}

func TestCommitmentRejectsInvalidManifest(t *testing.T) {
	m := fixture(t)
	m.Size++

	if _, err := m.Commitment(); err == nil {
		t.Fatal("accepted inconsistent file size")
	}
}
