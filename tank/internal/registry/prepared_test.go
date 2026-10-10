package registry

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"math/big"
	"path/filepath"
	"strings"
	"sync/atomic"
	"tank.local/tank/internal/metadata"
	"testing"
	"time"
)

type offlineSigner struct {
	key                    *ecdsa.PrivateKey
	calls                  atomic.Int32
	mutate                 func(*types.DynamicFeeTx)
	failure                bool
	corruptCachedSignature bool
	otherKey               *ecdsa.PrivateKey
}

func (s *offlineSigner) Address() common.Address { return crypto.PubkeyToAddress(s.key.PublicKey) }
func (s *offlineSigner) SignTransaction(ctx context.Context, tx *types.Transaction, chain *big.Int) (*types.Transaction, error) {
	s.calls.Add(1)
	if s.failure {
		return nil, errors.New("synthetic-wallet-secret-do-not-echo")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.mutate != nil {
		copy := &types.DynamicFeeTx{ChainID: tx.ChainId(), Nonce: tx.Nonce(), Gas: tx.Gas(), GasFeeCap: tx.GasFeeCap(), GasTipCap: tx.GasTipCap(), To: tx.To(), Value: tx.Value(), Data: tx.Data(), AccessList: tx.AccessList()}
		s.mutate(copy)
		tx = types.NewTx(copy)
		chain = tx.ChainId()
	}
	key := s.key
	if s.otherKey != nil {
		key = s.otherKey
	}
	signed, err := types.SignTx(tx, types.LatestSignerForChainID(chain), key)
	if err == nil && s.corruptCachedSignature {
		_, _ = types.Sender(types.LatestSignerForChainID(chain), signed)
		_, r, _ := signed.RawSignatureValues()
		r.SetInt64(0)
	}
	return signed, err
}
func signerFixture(t *testing.T) *offlineSigner {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal("test-only signer generation failed")
	}
	return &offlineSigner{key: key}
}
func policyFixture(signer TransactionSigner) TestnetPolicy {
	return TestnetPolicy{Contract: common.HexToAddress("0x1111111111111111111111111111111111111111"), Registrant: signer.Address(), MaxGas: 100000, MaxFeeWei: big.NewInt(10000000000), MaxTipWei: big.NewInt(1000000000), MaxExecutionCostWei: big.NewInt(1000000000000000), Confirmations: 12}
}
func termsFixture() TransactionTerms {
	return TransactionTerms{Nonce: 0, Gas: 50000, FeeWei: big.NewInt(2000000000), TipWei: big.NewInt(100000000)}
}
func prepareJob(t *testing.T, store *metadata.Store, policy TestnetPolicy, label string, lease time.Duration) metadata.RegistrationJob {
	t.Helper()
	ctx := context.Background()
	m := workerManifest(t, label)
	if err := store.Save(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := store.EnqueueRegistration(ctx, m.FileID, policy.target()); err != nil {
		t.Fatal(err)
	}
	job, err := store.ClaimRegistration(ctx, policy.target(), lease)
	if err != nil {
		t.Fatal(err)
	}
	return job
}
func preparationContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func TestPreparedTransactionSurvivesRestartAndLeaseExpiryWithoutSigningAgain(t *testing.T) {
	ctx := preparationContext(t)
	path := filepath.Join(t.TempDir(), "journal.sqlite")
	store, err := metadata.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	signer := signerFixture(t)
	policy := policyFixture(signer)
	job := prepareJob(t, store, policy, "prepared resume", time.Second)
	prepared, err := PrepareTestnetRegistration(ctx, store, job, policy, signer, termsFixture())
	if err != nil {
		t.Fatal(err)
	}
	if signer.calls.Load() != 1 || len(prepared.Raw) == 0 {
		t.Fatal("preparation did not persist signed bytes")
	}
	status, err := store.GetRegistrationStatus(ctx, job.FileID, job.Target)
	if err != nil || status.State != "pending" || status.TransactionHash != prepared.Hash {
		t.Fatal("pre-broadcast intent was not durably recorded as pending")
	}
	store.Close()
	store, err = metadata.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	time.Sleep(1100 * time.Millisecond)
	if _, err := PrepareTestnetRegistration(ctx, store, job, policy, signer, termsFixture()); !errors.Is(err, metadata.ErrLeaseLost) {
		t.Fatal("stale lease accessed preparation")
	}
	resumed, err := store.ClaimRegistration(ctx, policy.target(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	signer.failure = true
	newTerms := termsFixture()
	newTerms.Nonce = 999
	restored, err := PrepareTestnetRegistration(ctx, store, resumed, policy, signer, newTerms)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Hash != prepared.Hash || string(restored.Raw) != string(prepared.Raw) || restored.Nonce != prepared.Nonce || signer.calls.Load() != 1 {
		t.Fatal("restart changed or resigned the outstanding transaction")
	}
}
func TestPreparedRejectsAlteredSignerIntentAndRedactsFailures(t *testing.T) {
	mutations := map[string]func(*types.DynamicFeeTx){
		"nonce": func(tx *types.DynamicFeeTx) { tx.Nonce++ }, "gas": func(tx *types.DynamicFeeTx) { tx.Gas-- },
		"fee": func(tx *types.DynamicFeeTx) { tx.GasFeeCap.Add(tx.GasFeeCap, big.NewInt(1)) }, "tip": func(tx *types.DynamicFeeTx) { tx.GasTipCap.Add(tx.GasTipCap, big.NewInt(1)) },
		"value": func(tx *types.DynamicFeeTx) { tx.Value = big.NewInt(1) }, "chain": func(tx *types.DynamicFeeTx) { tx.ChainID = big.NewInt(8453) },
		"destination": func(tx *types.DynamicFeeTx) {
			address := common.HexToAddress("0x2222222222222222222222222222222222222222")
			tx.To = &address
		},
		"calldata": func(tx *types.DynamicFeeTx) { tx.Data[0] ^= 1 }, "access_list": func(tx *types.DynamicFeeTx) {
			tx.AccessList = types.AccessList{{Address: common.HexToAddress("0x3333333333333333333333333333333333333333")}}
		},
	}
	for name, mutation := range mutations {
		t.Run(name, func(t *testing.T) {
			ctx := preparationContext(t)
			store := workerStore(t)
			signer := signerFixture(t)
			signer.mutate = mutation
			policy := policyFixture(signer)
			job := prepareJob(t, store, policy, "altered intent", time.Minute)
			if _, err := PrepareTestnetRegistration(ctx, store, job, policy, signer, termsFixture()); !errors.Is(err, ErrSigning) {
				t.Fatal("altered signing intent accepted")
			}
			if _, err := store.PreparedRegistration(ctx, job, 84532, strings.ToLower(signer.Address().Hex())); !errors.Is(err, metadata.ErrNotFound) {
				t.Fatal("rejected signature persisted")
			}
		})
	}
	ctx := preparationContext(t)
	store := workerStore(t)
	signer := signerFixture(t)
	signer.failure = true
	policy := policyFixture(signer)
	job := prepareJob(t, store, policy, "signing error", time.Minute)
	_, err := PrepareTestnetRegistration(ctx, store, job, policy, signer, termsFixture())
	if !errors.Is(err, ErrSigning) || strings.Contains(err.Error(), "synthetic-wallet-secret") {
		t.Fatal("signing error was not safely redacted")
	}
}
func TestPreparedBoundsFeesGasBudgetAndContextBeforeSigning(t *testing.T) {
	for _, mode := range []string{"gas", "fee", "tip", "negative", "budget", "uint256", "unbounded"} {
		t.Run(mode, func(t *testing.T) {
			ctx := preparationContext(t)
			store := workerStore(t)
			signer := signerFixture(t)
			policy := policyFixture(signer)
			job := prepareJob(t, store, policy, "bounded fees", time.Minute)
			terms := termsFixture()
			switch mode {
			case "gas":
				terms.Gas = policy.MaxGas + 1
			case "fee":
				terms.FeeWei = new(big.Int).Add(policy.MaxFeeWei, big.NewInt(1))
			case "tip":
				terms.TipWei = new(big.Int).Add(policy.MaxTipWei, big.NewInt(1))
			case "negative":
				terms.FeeWei = big.NewInt(-1)
			case "budget":
				policy.MaxExecutionCostWei = big.NewInt(1)
			case "uint256":
				terms.FeeWei = new(big.Int).Lsh(big.NewInt(1), 256)
			case "unbounded":
				ctx = context.Background()
			}
			if _, err := PrepareTestnetRegistration(ctx, store, job, policy, signer, terms); !errors.Is(err, ErrPreparation) {
				t.Fatal("unbounded preparation accepted")
			}
			if signer.calls.Load() != 0 {
				t.Fatal("invalid policy contacted signer")
			}
		})
	}
}
func TestPreparedAccountLaneSpansContractsAndWorkers(t *testing.T) {
	ctx := preparationContext(t)
	path := filepath.Join(t.TempDir(), "lane.sqlite")
	first, err := metadata.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := metadata.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	signer := signerFixture(t)
	policyA := policyFixture(signer)
	policyB := policyFixture(signer)
	policyB.Contract = common.HexToAddress("0x2222222222222222222222222222222222222222")
	jobA := prepareJob(t, first, policyA, "lane A", time.Minute)
	jobB := prepareJob(t, second, policyB, "lane B", time.Minute)
	results := make(chan error, 2)
	go func() {
		_, err := PrepareTestnetRegistration(ctx, first, jobA, policyA, signer, termsFixture())
		results <- err
	}()
	go func() {
		_, err := PrepareTestnetRegistration(ctx, second, jobB, policyB, signer, termsFixture())
		results <- err
	}()
	successes, busy := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			successes++
		} else if errors.Is(err, metadata.ErrAccountBusy) {
			busy++
		} else {
			t.Fatal("account lane concurrency failed")
		}
	}
	if successes != 1 || busy != 1 {
		t.Fatal("same signer reserved two outstanding transactions across contracts")
	}
}
func TestConfirmationRejectsReorgReadbackAndDepthThenAdvancesNonce(t *testing.T) {
	ctx := preparationContext(t)
	store := workerStore(t)
	signer := signerFixture(t)
	policy := policyFixture(signer)
	job := prepareJob(t, store, policy, "confirmed intent", time.Minute)
	prepared, err := PrepareTestnetRegistration(ctx, store, job, policy, signer, termsFixture())
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := store.Load(ctx, job.FileID)
	if err != nil {
		t.Fatal(err)
	}
	commitment, err := manifest.Commitment()
	if err != nil {
		t.Fatal(err)
	}
	header := &types.Header{Number: big.NewInt(10), Time: 123, Extra: []byte("canonical fixture")}
	observation := ConfirmedObservation{Receipt: &types.Receipt{TxHash: common.HexToHash(prepared.Hash), Status: types.ReceiptStatusSuccessful, BlockHash: header.Hash(), BlockNumber: big.NewInt(10)}, CanonicalHeader: header, Head: 21, ReadbackBlockHash: header.Hash(), Record: Record{Commitment: commitment, FileSize: uint64(manifest.Size), RegisteredAt: 123}}
	for _, mode := range []string{"missing_receipt", "reverted", "transaction", "reorg", "depth", "future_block", "latest_readback", "commitment", "size", "nil_header", "header_number"} {
		copy := observation
		receipt := *observation.Receipt
		copy.Receipt = &receipt
		switch mode {
		case "missing_receipt":
			copy.Receipt = nil
		case "reverted":
			copy.Receipt.Status = types.ReceiptStatusFailed
		case "transaction":
			copy.Receipt.TxHash = common.HexToHash("0xdead")
		case "reorg":
			other := *header
			other.Extra = []byte("reorg")
			copy.CanonicalHeader = &other
		case "depth":
			copy.Head = 20
		case "future_block":
			copy.Head = 9
		case "latest_readback":
			copy.ReadbackBlockHash = common.HexToHash("0xbeef")
		case "commitment":
			copy.Record.Commitment[0] ^= 1
		case "size":
			copy.Record.FileSize++
		case "nil_header":
			copy.CanonicalHeader = nil
		case "header_number":
			other := *header
			other.Number = big.NewInt(11)
			copy.CanonicalHeader = &other
		}
		if err := ConfirmPreparedRegistration(ctx, store, job, policy, copy); !errors.Is(err, ErrConfirmation) {
			t.Fatal("unconfirmed or conflicting observation accepted: " + mode)
		}
	}
	stale := job
	stale.Token = "synthetic-stale-fence"
	if err := ConfirmPreparedRegistration(ctx, store, stale, policy, observation); !errors.Is(err, metadata.ErrLeaseLost) {
		t.Fatal("stale worker confirmed a transaction")
	}
	if err := ConfirmPreparedRegistration(ctx, store, job, policy, observation); err != nil {
		t.Fatal(err)
	}
	status, err := store.GetRegistrationStatus(ctx, job.FileID, job.Target)
	if err != nil || status.State != "registered" {
		t.Fatal("confirmed observation did not complete job")
	}
	next := prepareJob(t, store, policy, "next intent", time.Minute)
	if _, err := PrepareTestnetRegistration(ctx, store, next, policy, signer, termsFixture()); !errors.Is(err, metadata.ErrNonceConflict) {
		t.Fatal("confirmed nonce reused")
	}
	terms := termsFixture()
	terms.Nonce = 2
	if _, err := PrepareTestnetRegistration(ctx, store, next, policy, signer, terms); !errors.Is(err, metadata.ErrNonceConflict) {
		t.Fatal("external nonce change was silently skipped")
	}
	terms.Nonce = 1
	if _, err := PrepareTestnetRegistration(ctx, store, next, policy, signer, terms); err != nil {
		t.Fatal("next consecutive nonce blocked")
	}
}

func TestPreparedRejectsCachedSenderWithMutatedSignature(t *testing.T) {
	ctx := preparationContext(t)
	store := workerStore(t)
	signer := signerFixture(t)
	signer.corruptCachedSignature = true
	policy := policyFixture(signer)
	job := prepareJob(t, store, policy, "corrupt signature cache", time.Minute)
	if _, err := PrepareTestnetRegistration(ctx, store, job, policy, signer, termsFixture()); !errors.Is(err, ErrSigning) {
		t.Fatal("invalid signature passed cached sender validation")
	}
	if _, err := store.PreparedRegistration(ctx, job, 84532, strings.ToLower(signer.Address().Hex())); !errors.Is(err, metadata.ErrNotFound) {
		t.Fatal("invalid signature poisoned the account journal")
	}
}

func TestPreparedRejectsWrongSigningAccountAndMalformedJournal(t *testing.T) {
	ctx := preparationContext(t)
	store := workerStore(t)
	signer := signerFixture(t)
	policy := policyFixture(signer)
	job := prepareJob(t, store, policy, "wrong signing account", time.Minute)
	signer.otherKey = signerFixture(t).key
	if _, err := PrepareTestnetRegistration(ctx, store, job, policy, signer, termsFixture()); !errors.Is(err, ErrSigning) {
		t.Fatal("another account's signature accepted")
	}
	raw := []byte("synthetic corrupt signed journal")
	value := metadata.PreparedRegistration{ChainID: 84532, Account: strings.ToLower(policy.Registrant.Hex()), FileID: job.FileID, Target: job.Target, Hash: strings.ToLower(crypto.Keccak256Hash(raw).Hex()), Raw: raw}
	if err := store.SavePreparedRegistration(ctx, job, value); err != nil {
		t.Fatal(err)
	}
	calls := signer.calls.Load()
	if _, err := PrepareTestnetRegistration(ctx, store, job, policy, signer, termsFixture()); !errors.Is(err, ErrPreparation) {
		t.Fatal("malformed persisted intent accepted")
	}
	if signer.calls.Load() != calls {
		t.Fatal("malformed journal triggered a replacement signature")
	}
}
