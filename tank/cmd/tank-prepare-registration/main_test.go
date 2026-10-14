package main

import (
	"errors"
	"os"
	"path/filepath"
	"tank.local/tank/internal/registrationguard"
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

func TestMaintenanceAndConcurrentWorkerRejectBeforeCustody(t *testing.T) {
	previous := os.Args
	defer func() { os.Args = previous }()
	os.Args = []string{"tank-prepare-registration", "--broadcast"}
	directory := t.TempDir()
	os.Chmod(directory, 0700)
	database := filepath.Join(directory, "state.sqlite")
	for key, value := range map[string]string{
		"TANK_CHAIN_ID": "5042002", "TANK_REGISTRY_ADDR": "0x1111111111111111111111111111111111111111",
		"TANK_ARC_TESTNET_SENDER":  "0x2222222222222222222222222222222222222222",
		"TANK_ARC_TESTNET_MAX_GAS": "1000000", "TANK_ARC_TESTNET_MAX_FEE": "65000000000",
		"TANK_ARC_TESTNET_MAX_TIP": "1000000000", "TANK_ARC_TESTNET_MAX_COST": "8000000000000000",
		"TANK_ARC_TESTNET_CONFIRMATIONS": "1", "TANK_DATABASE_PATH": database,
		"TANK_REGISTRATION_LOCK_PATH": "", "TANK_SIGNER_KEYSTORE_FILE": "missing-custody",
	} {
		t.Setenv(key, value)
	}
	guard, err := registrationguard.Acquire(database + ".registration.lock")
	if err != nil {
		t.Fatal(err)
	}
	if err := run(); !errors.Is(err, registrationguard.ErrBlocked) {
		t.Fatal("concurrent worker reached custody")
	}
	guard.Close()
	if err := os.WriteFile(database+".registration.lock.restore-gate", []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(); !errors.Is(err, registrationguard.ErrBlocked) {
		t.Fatal("maintenance gate reached custody")
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
