package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifiedDownload(t *testing.T) {
	data := []byte("Tank verified download")
	sum := sha256.Sum256(data)
	id := hex.EncodeToString(sum[:])
	destination := filepath.Join(t.TempDir(), "output.bin")

	if _, err := saveVerified(bytes.NewReader(data), id, destination); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(destination)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("saved file mismatch: %v", err)
	}

	if _, err := saveVerified(bytes.NewReader(data), id, destination); err == nil {
		t.Fatal("overwrote an existing output file")
	}

	badDestination := filepath.Join(t.TempDir(), "bad.bin")
	if _, err := saveVerified(
		bytes.NewReader([]byte("altered")), id, badDestination,
	); err == nil {
		t.Fatal("accepted incorrect content")
	}

	if _, err := os.Stat(badDestination); !os.IsNotExist(err) {
		t.Fatal("published a file that failed verification")
	}
}
