package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"tank.local/tank/internal/metadata"
	"testing"
)

func TestCommandCreatesPrivateSnapshotWithoutCreatingMissingSources(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.sqlite")
	out := filepath.Join(dir, "out.sqlite")
	var output bytes.Buffer
	if err := run([]string{"--db", source, "--out", out}, &output); err == nil {
		t.Fatal("missing source accepted")
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatal("missing source created")
	}
	store, err := metadata.Open(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := run([]string{"--db", source, "--out", out}, &output); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(out); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("snapshot not private")
	}
	if err := run([]string{"--db", source, "--out", out}, &output); err == nil {
		t.Fatal("snapshot overwrite accepted")
	}
}
