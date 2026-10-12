//go:build arc_integration

package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/holiman/uint256"
	"tank.local/tank/internal/integrity"
	"tank.local/tank/internal/metadata"
)

func arcLocalNode(t *testing.T) (*rpc.Client, *ethclient.Client, string) {
	t.Helper()
	binary := os.Getenv("TANK_ARC_ANVIL_BIN")
	if binary == "" {
		t.Fatal("explicit Arc Anvil binary required")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	// No public RPC, fork, known development account or persistent user state.
	cmd := exec.Command(binary, "--network", "arc", "--host", "127.0.0.1", "--port", fmt.Sprint(port), "--chain-id", "5042002", "--accounts", "0", "--quiet", "--block-base-fee-per-gas", "20000000000", "--gas-limit", "30000000")
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal("Arc Anvil failed to start")
	}
	t.Cleanup(func() { cmd.Process.Signal(os.Interrupt); cmd.Wait() })
	endpoint := fmt.Sprintf("http://127.0.0.1:%d", port)
	client, err := rpc.Dial(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	chain := ethclient.NewClient(client)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for {
		id, err := chain.ChainID(ctx)
		if err == nil && id.Int64() == ArcTestnetChainID {
			return client, chain, endpoint
		}
		if ctx.Err() != nil {
			t.Fatal("Arc Anvil did not become ready")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestArcLocalTransactionTypesAndNativeValueRules(t *testing.T) {
	connection, chain, _ := arcLocalNode(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	signer := signerFixture(t)
	recipient := signerFixture(t).Address()
	funding := new(big.Int).Mul(big.NewInt(10), big.NewInt(1_000_000_000_000_000_000))
	if err := connection.CallContext(ctx, nil, "anvil_setBalance", signer.Address(), hexutil.EncodeBig(funding)); err != nil {
		t.Fatal(err)
	}
	fee := big.NewInt(40_000_000_000)
	id := big.NewInt(ArcTestnetChainID)
	for _, kind := range []uint8{types.LegacyTxType, types.AccessListTxType, types.DynamicFeeTxType, types.SetCodeTxType} {
		nonce, err := chain.PendingNonceAt(ctx, signer.Address())
		if err != nil {
			t.Fatal(err)
		}
		var tx *types.Transaction
		switch kind {
		case types.LegacyTxType:
			tx = types.NewTx(&types.LegacyTx{Nonce: nonce, To: &recipient, Gas: 100000, GasPrice: fee, Value: big.NewInt(1_000_000_000_000_000_000)})
		case types.AccessListTxType:
			tx = types.NewTx(&types.AccessListTx{ChainID: id, Nonce: nonce, To: &recipient, Gas: 100000, GasPrice: fee})
		case types.DynamicFeeTxType:
			tx = types.NewTx(&types.DynamicFeeTx{ChainID: id, Nonce: nonce, To: &recipient, Gas: 100000, GasFeeCap: fee, GasTipCap: big.NewInt(0)})
		case types.SetCodeTxType:
			authority := signerFixture(t)
			auth, err := types.SignSetCode(authority.key, types.SetCodeAuthorization{ChainID: *uint256.MustFromBig(id), Address: common.HexToAddress("0x1111111111111111111111111111111111111111")})
			if err != nil {
				t.Fatal("local authorization failed")
			}
			tx = types.NewTx(&types.SetCodeTx{ChainID: uint256.MustFromBig(id), Nonce: nonce, To: recipient, Gas: 100000, GasFeeCap: uint256.MustFromBig(fee), GasTipCap: uint256.NewInt(0), Value: uint256.NewInt(0), AuthList: []types.SetCodeAuthorization{auth}})
		}
		signed, err := signer.SignTransaction(ctx, tx, id)
		if err != nil {
			t.Fatal("local signing failed")
		}
		if err := chain.SendTransaction(ctx, signed); err != nil {
			t.Fatalf("Arc local type %d rejected", kind)
		}
		receipt := localReceipt(t, chain, signed.Hash())
		if receipt.Status != 1 || receipt.Type != kind {
			t.Fatalf("Arc local type %d failed", kind)
		}
		if kind == types.LegacyTxType {
			balance, err := chain.BalanceAt(ctx, recipient, nil)
			if err != nil || balance.Cmp(big.NewInt(1_000_000_000_000_000_000)) != 0 {
				t.Fatal("native USDC transfer used incorrect decimals")
			}
			found := false
			for _, log := range receipt.Logs {
				if log.Address == common.HexToAddress("0xfffffffffffffffffffffffffffffffffffffffe") {
					found = true
				}
			}
			if !found {
				t.Fatal("Arc native movement did not emit a system Transfer log")
			}
		}
	}
	zero := common.Address{}
	nonce, err := chain.PendingNonceAt(ctx, signer.Address())
	if err != nil {
		t.Fatal(err)
	}
	burn, err := signer.SignTransaction(ctx, types.NewTx(&types.DynamicFeeTx{ChainID: id, Nonce: nonce, To: &zero, Gas: 100000, GasFeeCap: fee, GasTipCap: big.NewInt(0), Value: big.NewInt(1)}), id)
	if err != nil {
		t.Fatal("local burn fixture signing failed")
	}
	if err := chain.SendTransaction(ctx, burn); err == nil {
		if receipt := localReceipt(t, chain, burn.Hash()); receipt.Status != types.ReceiptStatusFailed {
			t.Fatal("Arc accepted native burn to zero address")
		}
	}
}

func localReceipt(t *testing.T, chain *ethclient.Client, hash common.Hash) *types.Receipt {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		r, err := chain.TransactionReceipt(ctx, hash)
		if err == nil {
			return r
		}
		if ctx.Err() != nil {
			t.Fatal("isolated local receipt unavailable")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestArcLocalDeploymentSignedRegistrationRestartReadbackAndNativeGas(t *testing.T) {
	connection, chain, endpoint := arcLocalNode(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	keyPath, passPath, generated := keystoreFixture(t)
	signer, err := OpenKeystoreSigner(ctx, keyPath, passPath, generated.Address())
	if err != nil {
		t.Fatal(err)
	}
	defer signer.Close()
	funding := new(big.Int).Mul(big.NewInt(100), big.NewInt(1_000_000_000_000_000_000))
	if err := connection.CallContext(ctx, nil, "anvil_setBalance", signer.Address(), hexutil.EncodeBig(funding)); err != nil {
		t.Fatal("isolated funding failed")
	}
	artifact, err := os.ReadFile("../../contracts/out/TankRegistry.sol/TankRegistry.json")
	if err != nil {
		t.Fatal("build TankRegistry before integration test")
	}
	var compiled struct {
		Bytecode struct {
			Object string `json:"object"`
		} `json:"bytecode"`
	}
	if json.Unmarshal(artifact, &compiled) != nil || compiled.Bytecode.Object == "" {
		t.Fatal("invalid registry artifact")
	}
	creation := common.FromHex(compiled.Bytecode.Object)
	gas, err := chain.EstimateGas(ctx, ethereum.CallMsg{From: signer.Address(), Data: creation})
	if err != nil {
		t.Fatal("local deployment estimate failed")
	}
	fee := big.NewInt(40_000_000_000)
	// Deployment signing is test-only; production signer deliberately permits
	// only registry calls. The generated key belongs solely to this fresh node.
	deployment, err := generated.SignTransaction(ctx, types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(ArcTestnetChainID), Gas: gas + gas/5, GasFeeCap: fee, GasTipCap: big.NewInt(1_000_000_000), Data: creation}), big.NewInt(ArcTestnetChainID))
	if err != nil || chain.SendTransaction(ctx, deployment) != nil {
		t.Fatal("isolated deployment failed")
	}
	receipt := localReceipt(t, chain, deployment.Hash())
	if receipt.Status != 1 || receipt.ContractAddress == (common.Address{}) {
		t.Fatal("deployment reverted")
	}
	code, err := chain.CodeAt(ctx, receipt.ContractAddress, nil)
	if err != nil || len(code) == 0 {
		t.Fatal("deployed registry missing")
	}
	// Trusted test TLS endpoint forwards only to the newly spawned local node.
	u, _ := url.Parse(endpoint)
	proxy := httptest.NewTLSServer(httputil.NewSingleHostReverseProxy(u))
	defer proxy.Close()
	reader, err := openTestnetReader(ctx, Config{ChainID: ArcTestnetChainID, RPCURL: proxy.URL, Contract: receipt.ContractAddress.Hex(), Registrant: signer.Address().Hex()}, proxy.Client())
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	p := TestnetPolicy{ChainID: ArcTestnetChainID, Contract: receipt.ContractAddress, Registrant: signer.Address(), MaxGas: 200000, MaxFeeWei: fee, MaxTipWei: big.NewInt(1_000_000_000), MaxExecutionCostWei: big.NewInt(8_000_000_000_000_000), Confirmations: 1}
	path := filepath.Join(t.TempDir(), "journal.sqlite")
	store, err := metadata.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	m := workerManifest(t, "Arc signed recovery")
	if err := store.Save(ctx, m); err != nil {
		t.Fatal(err)
	}
	w, err := NewSignedWorker(store, p, signer, reader)
	if err != nil {
		t.Fatal(err)
	}
	w.retryDelay = time.Second
	if err := w.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if worked, err := w.Process(ctx); err != nil || !worked {
			t.Fatalf("preparation/retry failed: %v", err)
		}
		time.Sleep(time.Duration(i+1)*time.Second + 100*time.Millisecond)
	}
	status, err := store.GetRegistrationStatus(ctx, m.FileID, w.Target())
	if err != nil || status.TransactionHash == "" || status.State == "registered" {
		t.Fatal("unsigned or falsely confirmed state")
	}
	job, err := store.ClaimRegistration(ctx, w.Target(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	value, err := store.PreparedRegistration(ctx, job, p.ChainID, strings.ToLower(p.Registrant.Hex()))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RetryRegistration(ctx, job, time.Second, "local recovery fixture"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	var tx types.Transaction
	if tx.UnmarshalBinary(value.Raw) != nil || tx.Nonce() != 1 {
		t.Fatal("prepared nonce or raw bytes changed")
	}
	before, err := chain.BalanceAt(ctx, signer.Address(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := chain.SendTransaction(ctx, &tx); err != nil {
		t.Fatal("isolated signed registration failed")
	}
	registered := localReceipt(t, chain, tx.Hash())
	if registered.Status != 1 {
		t.Fatal("isolated registration reverted")
	}
	after, err := chain.BalanceAt(ctx, signer.Address(), nil)
	if err != nil {
		t.Fatal(err)
	}
	cost := new(big.Int).Mul(new(big.Int).SetUint64(registered.GasUsed), registered.EffectiveGasPrice)
	if new(big.Int).Sub(before, after).Cmp(cost) != 0 {
		t.Fatal("native USDC balance delta disagrees with receipt gas cost")
	}
	// Crash/reopen after local send, before completion. Resume persisted bytes;
	// confirmation must work without ever invoking custody/estimation again.
	store.Close()
	store, err = metadata.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	closedSigner := &offlineSigner{key: generated.key, failure: true}
	w, err = NewSignedWorker(store, p, closedSigner, reader)
	if err != nil {
		t.Fatal(err)
	}
	w.retryDelay = time.Second
	if worked, err := w.Process(ctx); err != nil || !worked {
		t.Fatal("restart reconciliation failed")
	}
	status, err = store.GetRegistrationStatus(ctx, m.FileID, w.Target())
	if err != nil || status.State != "registered" || status.TransactionHash != value.Hash || closedSigner.calls.Load() != 0 {
		t.Fatal("restart failed canonical readback or re-signed intent")
	}
	id, _ := integrity.ParseHash(m.FileID)
	observation, err := reader.Observation(ctx, tx.Hash(), id)
	if err != nil || observation.Record.FileSize != uint64(m.Size) {
		t.Fatal("hash-pinned registry readback failed")
	}
	if worked, err := w.Process(ctx); err != nil || worked {
		t.Fatal("completed registration was retried")
	}
	// Repeat exact bytes after an ambiguous send; nonce and record stay fixed.
	_ = chain.SendTransaction(ctx, &tx)
	nonce, err := chain.PendingNonceAt(ctx, signer.Address())
	if err != nil || nonce != 2 {
		t.Fatal("immutable retry advanced nonce")
	}
}
