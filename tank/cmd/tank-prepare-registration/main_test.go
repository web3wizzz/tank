package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"tank.local/tank/internal/metadata"
	"tank.local/tank/internal/registrationguard"
	"tank.local/tank/internal/registry"
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
	store, err := metadata.Open(context.Background(), database)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
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
	if err := privateJournal(path); err == nil {
		t.Fatal("missing authority accepted")
	}
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
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
			local := filepath.Join(t.TempDir(), "journal.sqlite")
			if suffix != "" {
				if err := os.WriteFile(local, nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			link := local + suffix
			if err := os.Symlink(filepath.Join(dir, "unowned-state"), link); err != nil {
				t.Fatal(err)
			}
			defer os.Remove(link)
			if err := privateJournal(local); err == nil {
				t.Fatal("symlink journal state accepted")
			}
		})
	}
}

func TestMissingAndEmptyAuthorityRejectBeforeCustody(t *testing.T) {
	previous := os.Args
	defer func() { os.Args = previous }()
	os.Args = []string{"tank-prepare-registration", "--broadcast"}
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	path := filepath.Join(dir, "state.sqlite")
	for key, value := range map[string]string{
		"TANK_CHAIN_ID": "5042002", "TANK_REGISTRY_ADDR": "0x1111111111111111111111111111111111111111",
		"TANK_ARC_TESTNET_SENDER":  "0x2222222222222222222222222222222222222222",
		"TANK_ARC_TESTNET_MAX_GAS": "1000000", "TANK_ARC_TESTNET_MAX_FEE": "65000000000",
		"TANK_ARC_TESTNET_MAX_TIP": "1000000000", "TANK_ARC_TESTNET_MAX_COST": "8000000000000000",
		"TANK_ARC_TESTNET_CONFIRMATIONS": "1", "TANK_DATABASE_PATH": path,
		"TANK_REGISTRATION_LOCK_PATH": "", "TANK_SIGNER_KEYSTORE_FILE": "missing-custody",
	} {
		t.Setenv(key, value)
	}
	if err := run(); !errors.Is(err, invalid) {
		t.Fatalf("missing authority reached custody: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("bootstrapped missing authority")
	}
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(); !errors.Is(err, authorityUnavailable) {
		t.Fatalf("empty authority reached custody: %v", err)
	}
}

func TestInvalidArtifactRejectsBeforeCustodyAndRPC(t *testing.T) {
	previous := os.Args
	t.Cleanup(func() { os.Args = previous })
	os.Args = []string{"tank-prepare-registration", "--broadcast"}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "state.sqlite")
	store, err := metadata.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	for key, value := range map[string]string{
		"TANK_CHAIN_ID": "5042002", "TANK_REGISTRY_ADDR": "0x1111111111111111111111111111111111111111",
		"TANK_ARC_TESTNET_SENDER":  "0x2222222222222222222222222222222222222222",
		"TANK_ARC_TESTNET_MAX_GAS": "1000000", "TANK_ARC_TESTNET_MAX_FEE": "65000000000",
		"TANK_ARC_TESTNET_MAX_TIP": "1000000000", "TANK_ARC_TESTNET_MAX_COST": "8000000000000000",
		"TANK_ARC_TESTNET_CONFIRMATIONS": "1", "TANK_DATABASE_PATH": path,
		"TANK_REGISTRATION_LOCK_PATH": "", "TANK_ARC_TESTNET_TOTAL_BUDGET": "100000000000000000",
		"TANK_ARC_DEPLOY_ARTIFACT":  filepath.Join(dir, "missing-artifact"),
		"TANK_SIGNER_KEYSTORE_FILE": filepath.Join(dir, "missing-custody"),
		"TANK_SIGNER_PASSWORD_FILE": filepath.Join(dir, "missing-password"),
		"TANK_ARC_TESTNET_RPC":      "https://unreachable.invalid",
	} {
		t.Setenv(key, value)
	}
	if err := run(); !errors.Is(err, registry.ErrDeployment) {
		t.Fatal("invalid artifact reached custody/RPC setup", err)
	}
	t.Setenv("TANK_ARC_TESTNET_TOTAL_BUDGET", "invalid")
	if err := run(); !errors.Is(err, invalid) {
		t.Fatal("invalid budget reached artifact/custody setup", err)
	}
}
