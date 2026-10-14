package main

import (
	"os"
	"testing"
)

func TestRestoreCLIRejectsMainnetAndInvalidArguments(t *testing.T) {
	original := os.Args
	t.Cleanup(func() { os.Args = original })
	os.Args = []string{"tank-restore-check", "--db", "/does/not/exist"}
	t.Setenv("TANK_CHAIN_ID", "1")
	if run() == nil {
		t.Fatal("mainnet accepted")
	}
	t.Setenv("TANK_CHAIN_ID", "5042002")
	os.Args = []string{"tank-restore-check", "--broadcast"}
	if run() == nil {
		t.Fatal("broadcast argument accepted")
	}
	os.Args = []string{"tank-restore-check", "--db", "/does/not/exist"}
	t.Setenv("TANK_REGISTRY_ADDR", "bad")
	if run() == nil {
		t.Fatal("invalid configuration accepted")
	}
	t.Setenv("TANK_REGISTRY_ADDR", "0x1111111111111111111111111111111111111111")
	t.Setenv("TANK_ARC_TESTNET_SENDER", pilotSender)
	if run() == nil {
		t.Fatal("different registry accepted")
	}
	t.Setenv("TANK_REGISTRY_ADDR", pilotRegistry)
	t.Setenv("TANK_ARC_TESTNET_SENDER", "0x2222222222222222222222222222222222222222")
	if run() == nil {
		t.Fatal("different account accepted")
	}
}
