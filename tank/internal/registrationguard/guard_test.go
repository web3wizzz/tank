package registrationguard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConcurrentAndInterruptedGate(t *testing.T) {
	directory := t.TempDir()
	os.Chmod(directory, 0700)
	path := filepath.Join(directory, "lane.lock")
	first, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := Acquire(path); err == nil {
		second.Close()
		t.Fatal("concurrent signer admitted")
	}
	if err := os.WriteFile(path+".restore-gate", []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	first.Close()
	if second, err := Acquire(path); err == nil {
		second.Close()
		t.Fatal("interrupted restore admitted signer")
	}
	os.Remove(path + ".restore-gate") // Only the synthetic marker owned by this test.
	retry, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	retry.Close()
}

func TestAliasesAndUnsafePermissionsRefused(t *testing.T) {
	directory := t.TempDir()
	os.Chmod(directory, 0700)
	target := filepath.Join(directory, "target")
	os.WriteFile(target, nil, 0600)
	link := filepath.Join(directory, "link")
	os.Symlink(target, link)
	if g, err := Acquire(link); err == nil {
		g.Close()
		t.Fatal("symlink admitted")
	}
	os.Chmod(target, 0644)
	if g, err := Acquire(target); err == nil {
		g.Close()
		t.Fatal("public lane admitted")
	}
}
