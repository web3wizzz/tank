// tank-prepare-registration defaults to preparation. The explicit --broadcast
// mode activates the separately guarded Arc Testnet immutable-byte submitter.
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
	"tank.local/tank/internal/registrationguard"
	"tank.local/tank/internal/registry"
	"time"
)

var authorityUnavailable = errors.New("private initialized metadata journal unavailable")

var invalid = errors.New("Arc registration requires explicit bounded private testnet configuration")

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
			if suffix == "" {
				return invalid
			}
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
	broadcast := len(os.Args) == 2 && os.Args[1] == "--broadcast"
	if (len(os.Args) != 1 && !broadcast) || os.Getenv("TANK_CHAIN_ID") != "5042002" {
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
	lane := os.Getenv("TANK_REGISTRATION_LOCK_PATH")
	if lane == "" {
		lane = os.Getenv("TANK_DATABASE_PATH") + ".registration.lock"
	}
	guard, err := registrationguard.Acquire(lane)
	if err != nil {
		return err
	}
	defer guard.Close()
	// A restore candidate remains blocked even when a custom stable lane is used.
	if _, err := os.Lstat(os.Getenv("TANK_DATABASE_PATH") + ".restore-gate"); !os.IsNotExist(err) {
		return registrationguard.ErrBlocked
	}
	p := registry.TestnetPolicy{ChainID: registry.ArcTestnetChainID, Contract: common.HexToAddress(contract), Registrant: common.HexToAddress(account), MaxGas: gas, MaxFeeWei: fee, MaxTipWei: tip, MaxExecutionCostWei: cost, Confirmations: 1}
	// Operator must own a mode0700 parent directory for the journal and backups.
	store, err := metadata.OpenExisting(ctx, os.Getenv("TANK_DATABASE_PATH"))
	if err != nil {
		return authorityUnavailable
	}
	defer store.Close()
	var runtime []byte
	var budget *big.Int
	if broadcast {
		var ok bool
		budget, ok = new(big.Int).SetString(os.Getenv("TANK_ARC_TESTNET_TOTAL_BUDGET"), 10)
		if !ok || budget.Sign() <= 0 || budget.BitLen() > 256 {
			return invalid
		}
		_, runtime, err = registry.LoadDeploymentArtifact(os.Getenv("TANK_ARC_DEPLOY_ARTIFACT"))
		if err != nil {
			return err
		}
	}
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
	var w *registry.SignedWorker
	if broadcast {
		if err := reader.VerifyRuntime(ctx, runtime); err != nil {
			return err
		}
		w, err = registry.NewBroadcastSignedWorker(store, p, signer, reader, reader, budget)
	} else {
		w, err = registry.NewSignedWorker(store, p, signer, reader)
	}
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
	if len(os.Args) == 2 && os.Args[1] == "--broadcast" {
		fmt.Println("Arc Testnet registration pass finished; check canonical registration status for completion.")
	} else {
		fmt.Println("Arc intents prepared/reconciled privately; no transaction broadcast.")
	}
}
