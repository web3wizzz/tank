// tank-deploy-arc permits only explicitly configured Arc testnet creation.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"math/big"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"tank.local/tank/internal/registry"
	"time"
)

func run() error {
	if len(os.Args) != 2 || (os.Args[1] != "preflight" && os.Args[1] != "deploy") || os.Getenv("TANK_CHAIN_ID") != "5042002" {
		return registry.ErrDeployment
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	account := os.Getenv("TANK_ARC_TESTNET_SENDER")
	if !common.IsHexAddress(account) {
		return registry.ErrDeployment
	}
	gas, e := strconv.ParseUint(os.Getenv("TANK_ARC_DEPLOY_MAX_GAS"), 10, 64)
	if e != nil {
		return registry.ErrDeployment
	}
	fee, ok := new(big.Int).SetString(os.Getenv("TANK_ARC_DEPLOY_MAX_FEE"), 10)
	if !ok {
		return registry.ErrDeployment
	}
	tip, ok := new(big.Int).SetString(os.Getenv("TANK_ARC_DEPLOY_MAX_TIP"), 10)
	if !ok {
		return registry.ErrDeployment
	}
	cost, ok := new(big.Int).SetString(os.Getenv("TANK_ARC_DEPLOY_MAX_COST"), 10)
	if !ok {
		return registry.ErrDeployment
	}
	creation, runtime, e := registry.LoadDeploymentArtifact(os.Getenv("TANK_ARC_DEPLOY_ARTIFACT"))
	if e != nil {
		return e
	}
	signer, e := registry.OpenKeystoreSigner(ctx, os.Getenv("TANK_SIGNER_KEYSTORE_FILE"), os.Getenv("TANK_SIGNER_PASSWORD_FILE"), common.HexToAddress(account))
	if e != nil {
		return e
	}
	defer signer.Close()
	result, e := registry.DeployArc(ctx, registry.DeploymentConfig{ChainID: registry.ArcTestnetChainID, RPCURL: os.Getenv("TANK_ARC_TESTNET_RPC"), Sender: signer.Address(), Creation: creation, Runtime: runtime, MaxGas: gas, MaxFee: fee, MaxTip: tip, MaxCost: cost, Journal: os.Getenv("TANK_ARC_DEPLOY_JOURNAL")}, signer, os.Args[1] == "deploy")
	// Public transaction identifiers remain available even after RPC timeout.
	if result.Transaction != "" || e == nil {
		if json.NewEncoder(os.Stdout).Encode(result) != nil {
			return errors.New("public deployment result output failed")
		}
	}
	return e
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
