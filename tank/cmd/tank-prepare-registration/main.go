// tank-prepare-registration signs/journals and observes only. No broadcast API
// is linked into this workflow. Configure custody through private files.
package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"math/big"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"tank.local/tank/internal/metadata"
	"tank.local/tank/internal/registry"
	"time"
)

var invalid = errors.New("Arc preparation requires explicit private configuration; broadcasting is disabled")

func privateJournal(path string) error {
	if path == "" {
		return invalid
	}
	info, err := os.Lstat(filepath.Dir(path))
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return invalid
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return invalid
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		info, err := os.Lstat(path + suffix)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() {
			return invalid
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != uint32(os.Geteuid()) {
			return invalid
		}
	}
	return nil
}

func run() error {
	if len(os.Args) != 1 || os.Getenv("TANK_CHAIN_ID") != "5042002" {
		return invalid
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	contract, account := os.Getenv("TANK_REGISTRY_ADDR"), os.Getenv("TANK_ARC_TESTNET_SENDER")
	if !common.IsHexAddress(contract) || !common.IsHexAddress(account) {
		return invalid
	}
	gas, err := strconv.ParseUint(os.Getenv("TANK_ARC_TESTNET_MAX_GAS"), 10, 64)
	if err != nil {
		return invalid
	}
	fee, ok := new(big.Int).SetString(os.Getenv("TANK_ARC_TESTNET_MAX_FEE"), 10)
	if !ok {
		return invalid
	}
	tip, ok := new(big.Int).SetString(os.Getenv("TANK_ARC_TESTNET_MAX_TIP"), 10)
	if !ok {
		return invalid
	}
	cost, ok := new(big.Int).SetString(os.Getenv("TANK_ARC_TESTNET_MAX_COST"), 10)
	if !ok {
		return invalid
	}
	if os.Getenv("TANK_ARC_TESTNET_CONFIRMATIONS") != "1" || os.Getenv("TANK_DATABASE_PATH") == "" {
		return invalid
	}
	if err := privateJournal(os.Getenv("TANK_DATABASE_PATH")); err != nil {
		return err
	}
	p := registry.TestnetPolicy{ChainID: registry.ArcTestnetChainID, Contract: common.HexToAddress(contract), Registrant: common.HexToAddress(account), MaxGas: gas, MaxFeeWei: fee, MaxTipWei: tip, MaxExecutionCostWei: cost, Confirmations: 1}
	signer, err := registry.OpenKeystoreSigner(ctx, os.Getenv("TANK_SIGNER_KEYSTORE_FILE"), os.Getenv("TANK_SIGNER_PASSWORD_FILE"), p.Registrant)
	if err != nil {
		return err
	}
	defer signer.Close()
	reader, err := registry.OpenTestnetReader(ctx, registry.Config{ChainID: p.ChainID, RPCURL: os.Getenv("TANK_ARC_TESTNET_RPC"), Contract: contract, Registrant: account})
	if err != nil {
		return err
	}
	defer reader.Close()
	// Operator must own a mode0700 parent directory for the journal and backups.
	store, err := metadata.Open(ctx, os.Getenv("TANK_DATABASE_PATH"))
	if err != nil {
		return errors.New("private metadata journal unavailable")
	}
	defer store.Close()
	w, err := registry.NewSignedWorker(store, p, signer, reader)
	if err != nil {
		return err
	}
	if err := w.Reconcile(ctx); err != nil {
		return errors.New("private metadata reconciliation failed")
	}
	for {
		worked, err := w.Process(ctx)
		if err != nil {
			return errors.New("signed preparation interrupted or unavailable")
		}
		if !worked {
			return nil
		}
	}
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Arc intents prepared/reconciled privately; no transaction broadcast.")
}
