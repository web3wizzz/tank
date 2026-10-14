package registry

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"tank.local/tank/internal/integrity"
	"tank.local/tank/internal/metadata"
)

var ErrRestoreCheck = errors.New("restore chain or budget reconciliation failed; signing remains blocked")

// RestoreReader exposes no custody or transaction submission methods.
type RestoreReader interface {
	Observation(context.Context, common.Hash, integrity.Hash) (ConfirmedObservation, error)
	RestoreTransaction(context.Context, common.Hash) ([]byte, error)
	RestoreNonces(context.Context) (latest, pending uint64, err error)
}

// RestoreTransaction returns confirmed chain bytes, never a pending transaction.
func (r *TestnetReader) RestoreTransaction(ctx context.Context, hash common.Hash) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if hash == (common.Hash{}) || r.verifyNetwork(ctx) != nil {
		return nil, ErrTestnetRPC
	}
	transaction, pending, err := r.chain.TransactionByHash(ctx, hash)
	if err != nil || pending || transaction == nil {
		return nil, ErrTestnetRPC
	}
	raw, err := transaction.MarshalBinary()
	if err != nil || len(raw) > 4096 || r.verifyNetwork(ctx) != nil {
		return nil, ErrTestnetRPC
	}
	var fresh types.Transaction
	if fresh.UnmarshalBinary(raw) != nil || fresh.Hash() != hash {
		return nil, ErrTestnetRPC
	}
	return raw, nil
}

func (r *TestnetReader) RestoreNonces(ctx context.Context) (uint64, uint64, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if r.verifyNetwork(ctx) != nil {
		return 0, 0, ErrTestnetRPC
	}
	latest, e := r.chain.NonceAt(ctx, r.registrant, nil)
	if e != nil {
		return 0, 0, ErrTestnetRPC
	}
	pending, e := r.chain.PendingNonceAt(ctx, r.registrant)
	if e != nil || r.verifyNetwork(ctx) != nil {
		return 0, 0, ErrTestnetRPC
	}
	return latest, pending, nil
}

// CheckRestoreState is deliberately conservative: every signed intent must have
// canonical successful receipt/readback before maintenance can be released.
// Pending or ambiguous intents remain blocked, with their original bytes intact.
func CheckRestoreState(ctx context.Context, s metadata.RestoreState, p TestnetPolicy, reader RestoreReader) error {
	if reader == nil || !p.valid() || p.ChainID != ArcTestnetChainID || p.Confirmations != 1 || p.MaxGas > 1000000 || p.MaxTipWei.Cmp(big.NewInt(1000000000)) > 0 || p.MaxFeeWei.Cmp(big.NewInt(65000000000)) != 0 || p.MaxExecutionCostWei.Cmp(big.NewInt(8000000000000000)) != 0 {
		return ErrRestoreCheck
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	account := strings.ToLower(p.Registrant.Hex())
	jobs := map[string]metadata.RestoreJob{}
	byHash := map[string]metadata.RestoreJob{}
	observedCost := map[string]*big.Int{}
	signedCost := map[string]*big.Int{}
	chainBytes := map[string][]byte{}
	nonces := map[uint64]bool{}
	var maximumNonce uint64
	for _, j := range s.Jobs {
		if j.Target != p.target() || j.Manifest.Validate() != nil || j.Manifest.FileID != j.FileID {
			return ErrRestoreCheck
		}
		if _, ok := jobs[j.FileID]; ok {
			return ErrRestoreCheck
		}
		jobs[j.FileID] = j
		switch j.Status {
		case "pending", "submitted", "registered", "failed":
		default:
			return ErrRestoreCheck
		}
		if j.Hash == "" {
			if j.Status == "registered" || j.Status == "submitted" {
				return ErrRestoreCheck
			}
			continue
		}
		if len(j.Hash) != 66 || !strings.HasPrefix(j.Hash, "0x") || common.HexToHash(j.Hash).Hex() != j.Hash {
			return ErrRestoreCheck
		}
		if _, ok := byHash[j.Hash]; ok {
			return ErrRestoreCheck
		}
		byHash[j.Hash] = j
		raw, e := reader.RestoreTransaction(ctx, common.HexToHash(j.Hash))
		if e != nil || len(raw) > 4096 {
			return ErrRestoreCheck
		}
		var chainTx types.Transaction
		if chainTx.UnmarshalBinary(raw) != nil {
			return ErrRestoreCheck
		}
		data, e := registrationData(j.Manifest)
		if e != nil {
			return ErrRestoreCheck
		}
		intent := metadata.PreparedRegistration{ChainID: p.ChainID, Account: account, FileID: j.FileID, Target: j.Target, Nonce: chainTx.Nonce(), Hash: j.Hash, Raw: raw}
		transaction, e := decodePrepared(intent, p, data)
		if e != nil || nonces[transaction.Nonce()] || transaction.Nonce() == 0 {
			return ErrRestoreCheck
		}
		nonces[transaction.Nonce()] = true
		if transaction.Nonce() > maximumNonce {
			maximumNonce = transaction.Nonce()
		}
		signedCost[j.Hash] = new(big.Int).Mul(new(big.Int).SetUint64(transaction.Gas()), transaction.GasFeeCap())
		chainBytes[j.Hash] = raw
		id, e := integrity.ParseHash(j.FileID)
		if e != nil {
			return ErrRestoreCheck
		}
		o, e := reader.Observation(ctx, common.HexToHash(j.Hash), id)
		if e != nil || !validRestoreObservation(j, o) || !positive256(o.Receipt.EffectiveGasPrice) || o.Receipt.GasUsed == 0 || o.Receipt.GasUsed > transaction.Gas() || o.Receipt.EffectiveGasPrice.Cmp(transaction.GasFeeCap()) > 0 {
			return ErrRestoreCheck
		}
		observedCost[j.Hash] = new(big.Int).Mul(new(big.Int).SetUint64(o.Receipt.GasUsed), o.Receipt.EffectiveGasPrice)
	}
	budget := big.NewInt(100000000000000000)
	sum := new(big.Int)
	reservations := map[string]*big.Int{}
	for _, v := range s.Reservations {
		cost, ok := new(big.Int).SetString(v.Cost, 10)
		if !ok || cost.String() != v.Cost || cost.Sign() <= 0 || cost.Cmp(p.MaxExecutionCostWei) > 0 || v.ChainID != p.ChainID || v.Account != account {
			return ErrRestoreCheck
		}
		if actual, ok := observedCost[v.Hash]; !ok || actual.Cmp(cost) > 0 || signedCost[v.Hash].Cmp(cost) != 0 {
			return ErrRestoreCheck
		}
		if _, ok := reservations[v.Hash]; ok {
			return ErrRestoreCheck
		}
		reservations[v.Hash] = cost
		sum.Add(sum, cost)
	}
	if len(s.Accounts) != 1 || s.Accounts[0].ChainID != p.ChainID || s.Accounts[0].Account != account || s.Accounts[0].Budget != budget.String() || s.Accounts[0].Reserved != sum.String() || sum.Cmp(budget) > 0 {
		return ErrRestoreCheck
	}
	for hash := range byHash {
		if _, ok := reservations[hash]; !ok {
			return ErrRestoreCheck
		}
	}
	if len(s.Lanes) != 1 {
		return ErrRestoreCheck
	}
	lane := s.Lanes[0]
	j, ok := jobs[lane.FileID]
	if !ok || j.Hash != lane.Hash || (lane.State != "prepared" && lane.State != "confirmed") || (lane.State == "confirmed" && j.Status != "registered") {
		return ErrRestoreCheck
	}
	data, e := registrationData(j.Manifest)
	if e != nil {
		return ErrRestoreCheck
	}
	transaction, e := decodePrepared(lane.PreparedRegistration, p, data)
	if e != nil {
		return ErrRestoreCheck
	}
	cost := new(big.Int).Mul(new(big.Int).SetUint64(transaction.Gas()), transaction.GasFeeCap())
	reserved, ok := reservations[lane.Hash]
	if !ok || cost.Cmp(reserved) != 0 || lane.Nonce == ^uint64(0) || !bytes.Equal(chainBytes[lane.Hash], lane.Raw) || maximumNonce != lane.Nonce || uint64(len(nonces)) != maximumNonce {
		return ErrRestoreCheck
	}
	latest, pending, e := reader.RestoreNonces(ctx)
	if e != nil || latest != lane.Nonce+1 || pending != latest {
		return ErrRestoreCheck
	}
	return nil
}

func validRestoreObservation(j metadata.RestoreJob, o ConfirmedObservation) bool {
	r, h := o.Receipt, o.CanonicalHeader
	if r == nil || h == nil || r.Status != 1 || r.TxHash != common.HexToHash(j.Hash) || r.BlockNumber == nil || !r.BlockNumber.IsUint64() || h.Number == nil || h.Number.Cmp(r.BlockNumber) != 0 || h.Hash() != r.BlockHash || o.ReadbackBlockHash != r.BlockHash || o.Head < r.BlockNumber.Uint64() {
		return false
	}
	commitment, e := j.Manifest.Commitment()
	return e == nil && o.Record.Commitment == commitment && o.Record.FileSize == uint64(j.Manifest.Size)
}
