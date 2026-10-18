// tank-restore-check performs read-only reconciliation without loading a signer.
package main

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"tank.local/tank/internal/metadata"
	"tank.local/tank/internal/registry"
)

var invalid = errors.New("restore check requires bounded private Arc Testnet configuration")

// Published pilot identity in deploy/arc-testnet/registry.json. Restore cannot
// silently select another registry/account or invent a different nonce baseline.
const pilotRegistry = "0xe1fC5025261af42dfe04788c7a09DAd3ea1EC435"
const pilotSender = "0xFdE02e33287b161f797A3C2c63EE08c3673C591E"

func run() error {
	if len(os.Args) != 3 || os.Args[1] != "--db" || !filepath.IsAbs(os.Args[2]) || os.Getenv("TANK_CHAIN_ID") != "5042002" {
		return invalid
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	contract, account := os.Getenv("TANK_REGISTRY_ADDR"), os.Getenv("TANK_ARC_TESTNET_SENDER")
	if !common.IsHexAddress(contract) || !common.IsHexAddress(account) || common.HexToAddress(contract) != common.HexToAddress(pilotRegistry) || common.HexToAddress(account) != common.HexToAddress(pilotSender) {
		return invalid
	}
	gas, e := strconv.ParseUint(os.Getenv("TANK_ARC_TESTNET_MAX_GAS"), 10, 64)
	if e != nil {
		return invalid
	}
	integer := func(name string) *big.Int {
		v, ok := new(big.Int).SetString(os.Getenv(name), 10)
		if !ok {
			return nil
		}
		return v
	}
	fee, tip, cost := integer("TANK_ARC_TESTNET_MAX_FEE"), integer("TANK_ARC_TESTNET_MAX_TIP"), integer("TANK_ARC_TESTNET_MAX_COST")
	if fee == nil || tip == nil || cost == nil || os.Getenv("TANK_ARC_TESTNET_CONFIRMATIONS") != "1" || os.Getenv("TANK_ARC_TESTNET_TOTAL_BUDGET") != "100000000000000000" {
		return invalid
	}
	p := registry.TestnetPolicy{ChainID: registry.ArcTestnetChainID, Contract: common.HexToAddress(contract), Registrant: common.HexToAddress(account), MaxGas: gas, MaxFeeWei: fee, MaxTipWei: tip, MaxExecutionCostWei: cost, Confirmations: 1}
	store, e := metadata.OpenRestoreReadOnly(ctx, os.Args[2])
	if e != nil {
		return invalid
	}
	defer store.Close()
	state, e := store.RestoreSnapshot(ctx)
	if e != nil {
		return invalid
	}
	_, runtime, e := registry.LoadDeploymentArtifact(os.Getenv("TANK_ARC_DEPLOY_ARTIFACT"))
	if e != nil {
		return invalid
	}
	reader, e := registry.OpenTestnetReader(ctx, registry.Config{ChainID: p.ChainID, RPCURL: os.Getenv("TANK_ARC_TESTNET_RPC"), Contract: contract, Registrant: account})
	if e != nil {
		return invalid
	}
	defer reader.Close()
	if reader.VerifyRuntime(ctx, runtime) != nil {
		return invalid
	}
	if registry.CheckRestoreState(ctx, state, p, reader) != nil {
		return invalid
	}
	return nil
}
func main() {
	if run() != nil {
		fmt.Fprintln(os.Stderr, "Restore reconciliation failed; signing remains blocked.")
		os.Exit(1)
	}
	fmt.Println("PASS: read-only Arc Testnet durable-state reconciliation; no signing or broadcast performed.")
}
