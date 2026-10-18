package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tank.local/tank/internal/metadata"
)

func TestCoordinatorRefusesInactiveDatabaseBeforeServing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inactive.sqlite")
	if err := os.WriteFile(path+".restore-gate", []byte("synthetic inactive checkpoint"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TANK_DATABASE_PATH", path)
	t.Setenv("TANK_REQUIRE_EXISTING_DATABASE", "false")
	t.Setenv("TANK_COORDINATOR_ADDR", "127.0.0.1:8080")
	t.Setenv("TANK_NODE_ADDR", "127.0.0.1:9101")
	// Invalid node configuration would be reached only after opening authority.
	t.Setenv("TANK_NODES", "")
	if err := run(); !errors.Is(err, metadata.ErrRestoreBlocked) {
		t.Fatalf("inactive authority was not rejected before service setup: %v", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("inactive bootstrap created a database")
	}
}

func TestNodeDomainRequirementIsExplicit(t *testing.T) {
	t.Setenv("TANK_NODES", "http://127.0.0.1:9101,http://127.0.0.1:9102,http://127.0.0.1:9103")
	t.Setenv("TANK_NODE_TOKEN", strings.Repeat("s", 32))
	t.Setenv("TANK_NODE_CREDENTIALS_FILE", "")
	t.Setenv("TANK_NODE_FAILURE_DOMAINS_FILE", "")
	t.Setenv("TANK_REQUIRE_NODE_FAILURE_DOMAINS", "true")
	if _, err := configuredNodes(); err == nil {
		t.Fatal("required domain inventory was missing")
	}
	t.Setenv("TANK_REQUIRE_NODE_FAILURE_DOMAINS", "invalid")
	if _, err := configuredNodes(); err == nil {
		t.Fatal("malformed requirement accepted")
	}
	t.Setenv("TANK_REQUIRE_NODE_FAILURE_DOMAINS", "false")
	if _, err := configuredNodes(); err != nil {
		t.Fatal("local fixture defaults rejected", err)
	}
}

func TestRegistrationModeRejectsUnknownAndMainnetWithoutRPC(t *testing.T) {
	t.Setenv("TANK_REGISTRATION_MODE", "unknown")
	if _, err := registrationHandler(nil, nil); err == nil {
		t.Fatal("unknown mode accepted")
	}
	t.Setenv("TANK_REGISTRATION_MODE", "arc-testnet-external")
	t.Setenv("TANK_REGISTRY_ADDR", "0x1111111111111111111111111111111111111111")
	t.Setenv("TANK_ARC_TESTNET_SENDER", "0x2222222222222222222222222222222222222222")
	t.Setenv("TANK_CHAIN_RPC", "https://unused.invalid/private-token")
	for _, chain := range []string{"", "invalid", "1", "5042", "31337"} {
		t.Setenv("TANK_CHAIN_ID", chain)
		if _, err := registrationHandler(nil, nil); err == nil {
			t.Fatalf("accepted non-testnet chain %s", chain)
		}
	}
}
