package main

import (
	"strings"
	"testing"
)

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
