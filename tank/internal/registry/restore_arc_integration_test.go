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
	"tank.local/tank/internal/metadata"
)

// Actual loopback Arc/TLS/SQLite proof; no public RPC, personal wallet or funds.
func TestArcLocalRestoreCanonicalProofAndUnchangedRetry(t *testing.T) {
	connection, chain, endpoint := arcLocalNode(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	key, password, generated := keystoreFixture(t)
	signer, err := OpenKeystoreSigner(ctx, key, password, generated.Address())
	if err != nil {
		t.Fatal(err)
	}
	defer signer.Close()
	if err := connection.CallContext(ctx, nil, "anvil_setBalance", signer.Address(), hexutil.EncodeBig(new(big.Int).Mul(big.NewInt(100), big.NewInt(1e18)))); err != nil {
		t.Fatal("local fixture funding failed")
	}
	creation, _, err := LoadDeploymentArtifact("../../contracts/out/TankRegistry.sol/TankRegistry.json")
	if err != nil {
		t.Fatal("local artifact unavailable")
	}
	deployment, err := signer.SignDeployment(ctx, types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(ArcTestnetChainID), Gas: 1000000, GasFeeCap: big.NewInt(40000000000), GasTipCap: big.NewInt(0), Value: big.NewInt(0), Data: creation}))
	if err != nil || chain.SendTransaction(ctx, deployment) != nil {
		t.Fatal("local fixture creation failed")
	}
	receipt := localReceipt(t, chain, deployment.Hash())
	if receipt.Status != 1 || receipt.ContractAddress == (common.Address{}) {
		t.Fatal("local creation reverted")
	}
	endpointURL, _ := url.Parse(endpoint)
	proxy := httptest.NewTLSServer(httputil.NewSingleHostReverseProxy(endpointURL))
	defer proxy.Close()
	reader, err := openTestnetReader(ctx, Config{ChainID: ArcTestnetChainID, RPCURL: proxy.URL, Contract: receipt.ContractAddress.Hex(), Registrant: signer.Address().Hex()}, proxy.Client())
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	policy := TestnetPolicy{ChainID: ArcTestnetChainID, Contract: receipt.ContractAddress, Registrant: signer.Address(), MaxGas: 200000, MaxFeeWei: big.NewInt(65000000000), MaxTipWei: big.NewInt(1000000000), MaxExecutionCostWei: big.NewInt(8000000000000000), Confirmations: 1}
	path := filepath.Join(t.TempDir(), "source.sqlite")
	store, err := metadata.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	custody := &countedCustody{TransactionSigner: signer}
	submitter := &acceptedAmbiguousSubmitter{reader: reader}
	worker, err := NewBroadcastSignedWorker(store, policy, custody, reader, submitter, big.NewInt(100000000000000000))
	if err != nil {
		t.Fatal(err)
	}
	worker.retryDelay = time.Second
	manifest := workerManifest(t, "isolated restore immutable registration")
	if store.Save(ctx, manifest) != nil || worker.Reconcile(ctx) != nil {
		t.Fatal("fixture setup failed")
	}
	if worked, err := worker.Process(ctx); err != nil || !worked {
		t.Fatal("local intent failed")
	}
	status, err := store.GetRegistrationStatus(ctx, manifest.FileID, worker.Target())
	if err != nil {
		t.Fatal(err)
	}
	localReceipt(t, chain, common.HexToHash(status.TransactionHash))
	state, err := store.RestoreSnapshot(ctx)
	if err != nil || len(state.Lanes) != 1 {
		t.Fatal("durable state missing")
	}
	original := append([]byte(nil), state.Lanes[0].Raw...)
	snapshot := filepath.Join(t.TempDir(), "restore.sqlite")
	if store.Backup(ctx, snapshot) != nil {
		t.Fatal("snapshot failed")
	}
	candidate, err := metadata.OpenRestoreReadOnly(ctx, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := candidate.RestoreSnapshot(ctx)
	candidate.Close()
	if err != nil || CheckRestoreState(ctx, saved, policy, reader) != nil || !bytes.Equal(saved.Lanes[0].Raw, original) {
		t.Fatal("canonical restore proof failed")
	}
	store.Close()
	store, err = metadata.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	custody.refuse = true
	worker, err = NewBroadcastSignedWorker(store, policy, custody, reader, submitter, big.NewInt(100000000000000000))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	if worked, err := worker.Process(ctx); err != nil || !worked {
		t.Fatal("unchanged retry failed")
	}
	recovered, _ := store.GetRegistrationStatus(ctx, manifest.FileID, worker.Target())
	checked, err := store.RestoreSnapshot(ctx)
	if err != nil || CheckRestoreState(ctx, checked, policy, reader) != nil || recovered.State != "registered" || recovered.TransactionHash != status.TransactionHash || !bytes.Equal(checked.Lanes[0].Raw, original) || custody.calls != 1 || submitter.calls != 1 {
		t.Fatal("restore/retry changed bytes or duplicated registration")
	}
	nonce, err := chain.PendingNonceAt(ctx, signer.Address())
	if err != nil || nonce != 2 {
		t.Fatal("duplicate transaction consumed a nonce")
	}
}
