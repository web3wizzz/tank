package main

import (
	"os"
	"path/filepath"
	"testing"
)

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
