//go:build arc_integration

package registry

import (
	"bytes"
	"context"
	"math/big"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"tank.local/tank/internal/integrity"
	"tank.local/tank/internal/metadata"
)

type countedCustody struct {
	TransactionSigner
	calls  int
	refuse bool
}

func (s *countedCustody) SignTransaction(ctx context.Context, tx *types.Transaction, chain *big.Int) (*types.Transaction, error) {
	s.calls++
	if s.refuse {
		return nil, ErrCustody
	}
	return s.TransactionSigner.SignTransaction(ctx, tx, chain)
}

type acceptedAmbiguousSubmitter struct {
	reader *TestnetReader
	calls  int
}

func (s *acceptedAmbiguousSubmitter) Submit(ctx context.Context, tx *types.Transaction, policy TestnetPolicy) error {
	s.calls++
	if err := s.reader.Submit(ctx, tx, policy); err != nil {
		return err
	}
	// Simulate loss of the successful RPC response after actual local inclusion.
	return ErrSubmission
}

func TestArcLocalAuthorizedWorkerKeystoreBroadcastRestartAndSpendPersistence(t *testing.T) {
	connection, chain, endpoint := arcLocalNode(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	keyPath, passwordPath, generated := keystoreFixture(t)
	signer, err := OpenKeystoreSigner(ctx, keyPath, passwordPath, generated.Address())
	if err != nil {
		t.Fatal(err)
	}
	defer signer.Close()
	if err := connection.CallContext(ctx, nil, "anvil_setBalance", signer.Address(), hexutil.EncodeBig(new(big.Int).Mul(big.NewInt(100), big.NewInt(1e18)))); err != nil {
		t.Fatal("isolated funding failed")
	}
	creation, runtime, err := LoadDeploymentArtifact("../../contracts/out/TankRegistry.sol/TankRegistry.json")
	if err != nil {
		t.Fatal("build registry artifact before integration test")
	}
	fee := big.NewInt(40_000_000_000)
	deployment, err := signer.SignDeployment(ctx, types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(ArcTestnetChainID), Gas: 1_000_000, GasFeeCap: fee, GasTipCap: big.NewInt(1_000_000_000), Value: big.NewInt(0), Data: creation}))
	if err != nil || chain.SendTransaction(ctx, deployment) != nil {
		t.Fatal("isolated encrypted-keystore deployment failed")
	}
	receipt := localReceipt(t, chain, deployment.Hash())
	if receipt.Status != 1 || receipt.ContractAddress == (common.Address{}) {
		t.Fatal("isolated deployment reverted")
	}
	u, _ := url.Parse(endpoint)
	proxy := httptest.NewTLSServer(httputil.NewSingleHostReverseProxy(u))
	defer proxy.Close()
	reader, err := openTestnetReader(ctx, Config{ChainID: ArcTestnetChainID, RPCURL: proxy.URL, Contract: receipt.ContractAddress.Hex(), Registrant: signer.Address().Hex()}, proxy.Client())
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	code, err := chain.CodeAt(ctx, receipt.ContractAddress, nil)
	if err != nil || !bytes.Equal(code, runtime) {
		t.Fatal("deployed runtime did not match compiled registry")
	}
	policy := TestnetPolicy{ChainID: ArcTestnetChainID, Contract: receipt.ContractAddress, Registrant: signer.Address(), MaxGas: 200_000, MaxFeeWei: fee, MaxTipWei: big.NewInt(1_000_000_000), MaxExecutionCostWei: big.NewInt(8_000_000_000_000_000), Confirmations: 1}
	budget := big.NewInt(8_000_000_000_000_000)
	path := filepath.Join(t.TempDir(), "broadcast.sqlite")
	store, err := metadata.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	custody := &countedCustody{TransactionSigner: signer}
	submitter := &acceptedAmbiguousSubmitter{reader: reader}
	worker, err := NewBroadcastSignedWorker(store, policy, custody, reader, submitter, budget)
	if err != nil {
		t.Fatal(err)
	}
	worker.retryDelay = time.Second
	m := workerManifest(t, "production broadcaster over trusted local TLS")
	if err := store.Save(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := worker.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if worked, err := worker.Process(ctx); err != nil || !worked {
		t.Fatal("authorized worker did not submit")
	}
	status, err := store.GetRegistrationStatus(ctx, m.FileID, worker.Target())
	if err != nil || status.State == "registered" || status.TransactionHash == "" || custody.calls != 1 || submitter.calls != 1 {
		t.Fatal("ambiguous acceptance lost journal or falsely completed")
	}
	registered := localReceipt(t, chain, common.HexToHash(status.TransactionHash))
	if registered.Status != 1 {
		t.Fatal("actual authorized registration failed")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = metadata.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	custody.refuse = true
	worker, err = NewBroadcastSignedWorker(store, policy, custody, reader, submitter, budget)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	if worked, err := worker.Process(ctx); err != nil || !worked {
		t.Fatal("restart did not recover")
	}
	recovered, err := store.GetRegistrationStatus(ctx, m.FileID, worker.Target())
	if err != nil || recovered.State != "registered" || recovered.TransactionHash != status.TransactionHash || custody.calls != 1 || submitter.calls != 1 {
		t.Fatal("restart changed transaction or invoked custody/submission")
	}
	id, _ := integrity.ParseHash(m.FileID)
	observation, err := reader.Observation(ctx, common.HexToHash(status.TransactionHash), id)
	commitment, _ := m.Commitment()
	if err != nil || observation.Record.Commitment != commitment || observation.Record.FileSize != uint64(m.Size) {
		t.Fatal("canonical on-chain commitment readback failed")
	}
	// A new process cannot silently raise the persisted aggregate budget.
	custody.refuse = false
	worker, err = NewBroadcastSignedWorker(store, policy, custody, reader, submitter, new(big.Int).Add(budget, big.NewInt(1)))
	if err != nil {
		t.Fatal(err)
	}
	next := workerManifest(t, "persistent budget cannot be raised on restart")
	if err := store.Save(ctx, next); err != nil {
		t.Fatal(err)
	}
	if err := worker.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if worked, err := worker.Process(ctx); err != nil || !worked {
		t.Fatal("budget reconciliation failed")
	}
	if submitter.calls != 1 {
		t.Fatal("changed aggregate budget reached submission")
	}
	nonce, err := chain.PendingNonceAt(ctx, signer.Address())
	if err != nil || nonce != 2 {
		t.Fatal("restart or exhausted policy advanced on-chain nonce")
	}
}
