package registry

import (
	"context"
	"errors"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"math/big"
	"strings"
	"tank.local/tank/internal/integrity"
	"tank.local/tank/internal/metadata"
	"time"
)

// TransactionSigner is supplied by the future selected custody adapter. It must
// honor ctx and only sign: it must not broadcast or print key/provider material.
type TransactionSigner interface {
	Address() common.Address
	SignTransaction(context.Context, *types.Transaction, *big.Int) (*types.Transaction, error)
}
type TestnetPolicy struct {
	Contract            common.Address
	Registrant          common.Address
	MaxGas              uint64
	MaxFeeWei           *big.Int
	MaxTipWei           *big.Int
	MaxExecutionCostWei *big.Int
	Confirmations       uint64
}
type TransactionTerms struct {
	Nonce, Gas     uint64
	FeeWei, TipWei *big.Int
}

var ErrPreparation = errors.New("invalid bounded testnet transaction preparation")
var ErrSigning = errors.New("transaction signing failed or changed its authorized intent")
var ErrConfirmation = errors.New("transaction is not canonically confirmed with matching registry readback")

func (p TestnetPolicy) target() string {
	return "84532:" + strings.ToLower(p.Contract.Hex()) + ":" + strings.ToLower(p.Registrant.Hex())
}
func (p TestnetPolicy) valid() bool {
	return p.Contract != (common.Address{}) && p.Registrant != (common.Address{}) && p.MaxGas >= 21000 && p.MaxGas <= 10000000 && positive256(p.MaxFeeWei) && nonnegative256(p.MaxTipWei) && p.MaxTipWei.Cmp(p.MaxFeeWei) <= 0 && positive256(p.MaxExecutionCostWei) && p.Confirmations >= 1 && p.Confirmations <= 1024
}
func positive256(v *big.Int) bool    { return v != nil && v.Sign() > 0 && v.BitLen() <= 256 }
func nonnegative256(v *big.Int) bool { return v != nil && v.Sign() >= 0 && v.BitLen() <= 256 }
func (p TestnetPolicy) allows(t *types.Transaction) bool {
	return t != nil && t.Type() == types.DynamicFeeTxType && t.ChainId().Cmp(big.NewInt(84532)) == 0 && t.To() != nil && *t.To() == p.Contract && t.Value().Sign() == 0 && t.Gas() >= 21000 && t.Gas() <= p.MaxGas && t.GasFeeCap().Cmp(p.MaxFeeWei) <= 0 && t.GasTipCap().Cmp(p.MaxTipWei) <= 0 && t.GasTipCap().Cmp(t.GasFeeCap()) <= 0 && positive256(t.GasFeeCap()) && nonnegative256(t.GasTipCap()) && new(big.Int).Mul(new(big.Int).SetUint64(t.Gas()), t.GasFeeCap()).Cmp(p.MaxExecutionCostWei) <= 0
}
func registrationData(m metadata.Manifest) ([]byte, error) {
	commitment, err := m.Commitment()
	if err != nil {
		return nil, ErrPreparation
	}
	id, err := integrity.ParseHash(m.FileID)
	if err != nil {
		return nil, ErrPreparation
	}
	parsed, err := abi.JSON(strings.NewReader(contractABI))
	if err != nil {
		return nil, ErrPreparation
	}
	return parsed.Pack("tank", [32]byte(id), [32]byte(commitment), uint64(m.Size))
}
func boundedPreparation(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	deadline, ok := ctx.Deadline()
	if !ok || deadline.After(time.Now().Add(90*time.Second)) {
		return ErrPreparation
	}
	return nil
}
func decodePrepared(value metadata.PreparedRegistration, p TestnetPolicy, data []byte) (*types.Transaction, error) {
	var transaction types.Transaction
	if err := transaction.UnmarshalBinary(value.Raw); err != nil || !p.allows(&transaction) || transaction.Nonce() != value.Nonce || strings.ToLower(transaction.Hash().Hex()) != value.Hash || !strings.EqualFold(value.Account, p.Registrant.Hex()) || value.ChainID != 84532 || value.Target != p.target() {
		return nil, ErrPreparation
	}
	sender, err := types.Sender(types.LatestSignerForChainID(big.NewInt(84532)), &transaction)
	if err != nil || sender != p.Registrant || !equalBytes(transaction.Data(), data) {
		return nil, ErrPreparation
	}
	return &transaction, nil
}
func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// PrepareTestnetRegistration has no RPC transport and cannot broadcast. It
// verifies a caller-provided signature and commits its immutable bytes first.
// Existing outstanding transactions resume byte-for-byte, never with a new nonce.
func PrepareTestnetRegistration(ctx context.Context, store *metadata.Store, job metadata.RegistrationJob, p TestnetPolicy, signer TransactionSigner, terms TransactionTerms) (metadata.PreparedRegistration, error) {
	var empty metadata.PreparedRegistration
	if err := boundedPreparation(ctx); err != nil {
		return empty, err
	}
	if store == nil || !p.valid() || signer == nil || signer.Address() != p.Registrant || job.Target != p.target() {
		return empty, ErrPreparation
	}
	manifest, err := store.Load(ctx, job.FileID)
	if err != nil {
		return empty, ErrPreparation
	}
	data, err := registrationData(manifest)
	if err != nil {
		return empty, err
	}
	existing, err := store.PreparedRegistration(ctx, job, 84532, strings.ToLower(p.Registrant.Hex()))
	if err == nil {
		if _, err := decodePrepared(existing, p, data); err != nil {
			return empty, err
		}
		return existing, nil
	}
	if !errors.Is(err, metadata.ErrNotFound) {
		return empty, journalError(err)
	}
	if !positive256(terms.FeeWei) || !nonnegative256(terms.TipWei) {
		return empty, ErrPreparation
	}
	unsigned := types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(84532), Nonce: terms.Nonce, Gas: terms.Gas, GasFeeCap: new(big.Int).Set(terms.FeeWei), GasTipCap: new(big.Int).Set(terms.TipWei), To: &p.Contract, Value: new(big.Int), Data: data})
	if !p.allows(unsigned) {
		return empty, ErrPreparation
	}
	transactionSigner := types.LatestSignerForChainID(big.NewInt(84532))
	// Data/access-list getters can share mutable storage: seal intent before the
	// untrusted custody callback rather than hashing its input afterwards.
	intendedHash := transactionSigner.Hash(unsigned)
	signed, err := signer.SignTransaction(ctx, unsigned, big.NewInt(84532))
	if ctx.Err() != nil {
		return empty, ctx.Err()
	}
	if err != nil || signed == nil {
		return empty, ErrSigning
	}
	// A signer may populate go-ethereum sender caches then mutate signature
	// pointers. Validate a fresh decoded byte snapshot, never that cached object.
	raw, err := signed.MarshalBinary()
	if err != nil {
		return empty, ErrSigning
	}
	var snapshot types.Transaction
	if err := snapshot.UnmarshalBinary(raw); err != nil {
		return empty, ErrSigning
	}
	sender, err := types.Sender(transactionSigner, &snapshot)
	if err != nil || sender != p.Registrant || transactionSigner.Hash(&snapshot) != intendedHash {
		return empty, ErrSigning
	}
	value := metadata.PreparedRegistration{ChainID: 84532, Account: strings.ToLower(p.Registrant.Hex()), FileID: job.FileID, Target: job.Target, Nonce: snapshot.Nonce(), Hash: strings.ToLower(snapshot.Hash().Hex()), Raw: raw}
	if err := store.SavePreparedRegistration(ctx, job, value); err != nil {
		return empty, journalError(err)
	}
	return value, nil
}
func journalError(err error) error {
	if errors.Is(err, metadata.ErrAccountBusy) || errors.Is(err, metadata.ErrNonceConflict) || errors.Is(err, metadata.ErrLeaseLost) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errors.New("transaction journal unavailable")
}

// ConfirmedObservation must be collected by the future trusted RPC adapter.
// Readback is pinned to the canonical receipt block, never unqualified latest.
// This is an L2 confirmation policy, not a proof of L1/rollup finality.
type ConfirmedObservation struct {
	Receipt           *types.Receipt
	CanonicalHeader   *types.Header
	Head              uint64
	ReadbackBlockHash common.Hash
	Record            Record
}

func ConfirmPreparedRegistration(ctx context.Context, store *metadata.Store, job metadata.RegistrationJob, p TestnetPolicy, observation ConfirmedObservation) error {
	if err := boundedPreparation(ctx); err != nil {
		return err
	}
	if store == nil || !p.valid() {
		return ErrPreparation
	}
	value, err := store.PreparedRegistration(ctx, job, 84532, strings.ToLower(p.Registrant.Hex()))
	if err != nil {
		return journalError(err)
	}
	manifest, err := store.Load(ctx, job.FileID)
	if err != nil {
		return ErrPreparation
	}
	data, err := registrationData(manifest)
	if err != nil {
		return err
	}
	transaction, err := decodePrepared(value, p, data)
	if err != nil {
		return err
	}
	receipt, header := observation.Receipt, observation.CanonicalHeader
	if receipt == nil || header == nil || receipt.Status != types.ReceiptStatusSuccessful || receipt.TxHash != transaction.Hash() || receipt.BlockNumber == nil || !receipt.BlockNumber.IsUint64() || header.Number == nil || header.Number.Cmp(receipt.BlockNumber) != 0 || header.Hash() != receipt.BlockHash || observation.ReadbackBlockHash != receipt.BlockHash {
		return ErrConfirmation
	}
	block := receipt.BlockNumber.Uint64()
	if observation.Head < block || observation.Head-block < p.Confirmations-1 {
		return ErrConfirmation
	}
	commitment, err := manifest.Commitment()
	if err != nil || observation.Record.Commitment != commitment || observation.Record.FileSize != uint64(manifest.Size) {
		return ErrConfirmation
	}
	if err := store.CompletePreparedRegistration(ctx, job, value); err != nil {
		return journalError(err)
	}
	return nil
}
