package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestBroadcastRejectsMainnetAndUnexpectedNetworksBeforeCustody(t *testing.T) {
	previous := os.Args
	defer func() { os.Args = previous }()
	os.Args = []string{"tank-prepare-registration", "--broadcast"}
	for _, chain := range []string{"5042", "1", "31337", "84532", "", "5042001"} {
		t.Setenv("TANK_CHAIN_ID", chain)
		if err := run(); !errors.Is(err, invalid) {
			t.Fatalf("unexpected network %q reached custody: %v", chain, err)
		}
	}
}

func TestPrivateJournalRejectsPublicDirectoryAndSymlinkState(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "journal.sqlite")
	if err := privateJournal(path); err != nil {
		t.Fatal("private new journal rejected")
	}
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := privateJournal(path); err == nil {
		t.Fatal("public journal directory accepted")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		t.Run(suffix, func(t *testing.T) {
			link := path + suffix
			if err := os.Symlink(filepath.Join(dir, "unowned-state"), link); err != nil {
				t.Fatal(err)
			}
			defer os.Remove(link)
			if err := privateJournal(path); err == nil {
				t.Fatal("symlink journal state accepted")
			}
		})
	}
}
