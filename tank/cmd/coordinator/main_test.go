package main

import "testing"

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
