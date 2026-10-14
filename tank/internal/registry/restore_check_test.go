package registry

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"tank.local/tank/internal/integrity"
	"tank.local/tank/internal/metadata"
)

type restoreReaderFixture struct {
	signer          *offlineSigner
	transactions    map[common.Hash][]byte
	observations    map[common.Hash]ConfirmedObservation
	observation     ConfirmedObservation
	raw             []byte
	transactionErr  error
	latest, pending uint64
	err             error
	calls           int
}

func (r *restoreReaderFixture) RestoreTransaction(_ context.Context, hash common.Hash) ([]byte, error) {
	if r.transactions != nil {
		return r.transactions[hash], r.transactionErr
	}
	return r.raw, r.transactionErr
}
func (r *restoreReaderFixture) Observation(_ context.Context, hash common.Hash, _ integrity.Hash) (ConfirmedObservation, error) {
	r.calls++
	if r.observations != nil {
		return r.observations[hash], r.err
	}
	return r.observation, r.err
}
func (r *restoreReaderFixture) RestoreNonces(context.Context) (uint64, uint64, error) {
	return r.latest, r.pending, r.err
}
func restoreFixture(t *testing.T) (metadata.RestoreState, TestnetPolicy, *restoreReaderFixture) {
	t.Helper()
	signer := signerFixture(t)
	p := policyFixture(signer)
	p.MaxFeeWei = big.NewInt(65000000000)
	p.MaxExecutionCostWei = big.NewInt(8000000000000000)
	p.Confirmations = 1
	m := workerManifest(t, "restore fixture")
	data, e := registrationData(m)
	if e != nil {
		t.Fatal(e)
	}
	tx, e := types.SignTx(types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(p.ChainID), Nonce: 1, Gas: 50000, GasFeeCap: big.NewInt(20000000000), GasTipCap: big.NewInt(0), To: &p.Contract, Data: data}), types.LatestSignerForChainID(big.NewInt(p.ChainID)), signer.key)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := tx.MarshalBinary()
	if e != nil {
		t.Fatal(e)
	}
	hash := tx.Hash().Hex()
	account := strings.ToLower(signer.Address().Hex())
	cost := new(big.Int).Mul(new(big.Int).SetUint64(tx.Gas()), tx.GasFeeCap()).String()
	s := metadata.RestoreState{Jobs: []metadata.RestoreJob{{FileID: m.FileID, Target: p.target(), Status: "registered", Hash: hash, Manifest: m}}, Lanes: []metadata.RestoreLane{{PreparedRegistration: metadata.PreparedRegistration{ChainID: p.ChainID, Account: account, FileID: m.FileID, Target: p.target(), Nonce: 1, Hash: hash, Raw: raw}, State: "confirmed"}}, Accounts: []metadata.RestoreAccount{{ChainID: p.ChainID, Account: account, Budget: "100000000000000000", Reserved: cost}}, Reservations: []metadata.RestoreReservation{{ChainID: p.ChainID, Account: account, Hash: hash, Cost: cost}}}
	h := &types.Header{Number: big.NewInt(5)}
	commitment, _ := m.Commitment()
	r := &restoreReaderFixture{signer: signer, raw: raw, latest: 2, pending: 2, observation: ConfirmedObservation{Receipt: &types.Receipt{Status: 1, GasUsed: 45000, EffectiveGasPrice: big.NewInt(20000000000), TxHash: tx.Hash(), BlockNumber: big.NewInt(5), BlockHash: h.Hash()}, CanonicalHeader: h, Head: 5, ReadbackBlockHash: h.Hash(), Record: Record{Commitment: commitment, FileSize: uint64(m.Size)}}}
	return s, p, r
}
func TestRestoreCheckCanonicalReadOnlyAndFailures(t *testing.T) {
	cases := map[string]func(*metadata.RestoreState, *TestnetPolicy, *restoreReaderFixture){
		"transaction unavailable": func(s *metadata.RestoreState, p *TestnetPolicy, r *restoreReaderFixture) {
			r.transactionErr = ErrTestnetRPC
		},
		"arbitrary historical reservation": func(s *metadata.RestoreState, p *TestnetPolicy, r *restoreReaderFixture) {
			s.Reservations[0].Cost = "900000000000000"
			s.Accounts[0].Reserved = s.Reservations[0].Cost
		},
		"wrong historical sender": func(s *metadata.RestoreState, p *TestnetPolicy, r *restoreReaderFixture) {
			rewriteRestoreTransaction(t, s, p, r, func(tx *types.DynamicFeeTx) {}, true)
		},
		"wrong historical target": func(s *metadata.RestoreState, p *TestnetPolicy, r *restoreReaderFixture) {
			rewriteRestoreTransaction(t, s, p, r, func(tx *types.DynamicFeeTx) {
				a := common.HexToAddress("0x2222222222222222222222222222222222222222")
				tx.To = &a
			}, false)
		},
		"wrong historical data": func(s *metadata.RestoreState, p *TestnetPolicy, r *restoreReaderFixture) {
			rewriteRestoreTransaction(t, s, p, r, func(tx *types.DynamicFeeTx) { tx.Data = []byte("unrelated") }, false)
		},
		"nonce history gap": func(s *metadata.RestoreState, p *TestnetPolicy, r *restoreReaderFixture) {
			rewriteRestoreTransaction(t, s, p, r, func(tx *types.DynamicFeeTx) { tx.Nonce = 2 }, false)
			r.latest = 3
			r.pending = 3
		},
		"missing receipt": func(s *metadata.RestoreState, p *TestnetPolicy, r *restoreReaderFixture) { r.err = ErrConfirmation },
		"ambiguous RPC": func(s *metadata.RestoreState, p *TestnetPolicy, r *restoreReaderFixture) {
			r.err = errors.New("secret provider failure")
		},
		"receipt exceeds reservation": func(s *metadata.RestoreState, p *TestnetPolicy, r *restoreReaderFixture) {
			r.observation.Receipt.GasUsed = 100000
		},
		"receipt price above signed cap": func(s *metadata.RestoreState, p *TestnetPolicy, r *restoreReaderFixture) {
			r.observation.Receipt.EffectiveGasPrice = big.NewInt(21000000000)
		},
		"missing effective gas price": func(s *metadata.RestoreState, p *TestnetPolicy, r *restoreReaderFixture) {
			r.observation.Receipt.EffectiveGasPrice = nil
		},
		"pending nonce conflict": func(s *metadata.RestoreState, p *TestnetPolicy, r *restoreReaderFixture) { r.pending++ },
		"latest nonce conflict":  func(s *metadata.RestoreState, p *TestnetPolicy, r *restoreReaderFixture) { r.latest++ },
		"budget history": func(s *metadata.RestoreState, p *TestnetPolicy, r *restoreReaderFixture) {
			s.Accounts[0].Reserved = "0"
		},
		"reservation absent": func(s *metadata.RestoreState, p *TestnetPolicy, r *restoreReaderFixture) { s.Reservations = nil },
		"reservation unrelated": func(s *metadata.RestoreState, p *TestnetPolicy, r *restoreReaderFixture) {
			s.Reservations[0].Hash = common.HexToHash("0xabc").Hex()
		},
		"signed bytes altered": func(s *metadata.RestoreState, p *TestnetPolicy, r *restoreReaderFixture) {
			s.Lanes[0].Raw = []byte("bad")
		},
		"noncanonical header": func(s *metadata.RestoreState, p *TestnetPolicy, r *restoreReaderFixture) {
			r.observation.CanonicalHeader = &types.Header{Number: big.NewInt(6)}
		},
		"commitment mismatch": func(s *metadata.RestoreState, p *TestnetPolicy, r *restoreReaderFixture) {
			r.observation.Record.FileSize++
		},
		"mainnet": func(s *metadata.RestoreState, p *TestnetPolicy, r *restoreReaderFixture) { p.ChainID = 1 },
		"raised budget": func(s *metadata.RestoreState, p *TestnetPolicy, r *restoreReaderFixture) {
			s.Accounts[0].Budget = "200000000000000000"
		},
		"registered without transaction": func(s *metadata.RestoreState, p *TestnetPolicy, r *restoreReaderFixture) { s.Jobs[0].Hash = "" },
		"duplicate registration": func(s *metadata.RestoreState, p *TestnetPolicy, r *restoreReaderFixture) {
			s.Jobs = append(s.Jobs, s.Jobs[0])
		},
	}
	s, p, r := restoreFixture(t)
	original := string(s.Lanes[0].Raw)
	if e := CheckRestoreState(context.Background(), s, p, r); e != nil {
		t.Fatal(e)
	}
	if original != string(s.Lanes[0].Raw) || r.calls != 1 {
		t.Fatal("validation changed durable bytes")
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			s, p, r := restoreFixture(t)
			change(&s, &p, r)
			if e := CheckRestoreState(context.Background(), s, p, r); !errors.Is(e, ErrRestoreCheck) {
				t.Fatal("unsafe reconciliation accepted")
			}
		})
	}
}

func rewriteRestoreTransaction(t *testing.T, s *metadata.RestoreState, p *TestnetPolicy, r *restoreReaderFixture, mutate func(*types.DynamicFeeTx), otherSender bool) {
	t.Helper()
	var old types.Transaction
	if old.UnmarshalBinary(r.raw) != nil {
		t.Fatal("fixture decode")
	}
	tx := &types.DynamicFeeTx{ChainID: old.ChainId(), Nonce: old.Nonce(), Gas: old.Gas(), GasFeeCap: old.GasFeeCap(), GasTipCap: old.GasTipCap(), To: old.To(), Data: old.Data()}
	mutate(tx)
	key := r.signer.key
	if otherSender {
		key = signerFixture(t).key
	}
	signed, e := types.SignTx(types.NewTx(tx), types.LatestSignerForChainID(big.NewInt(p.ChainID)), key)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := signed.MarshalBinary()
	if e != nil {
		t.Fatal(e)
	}
	r.raw = raw
	s.Jobs[0].Hash = signed.Hash().Hex()
	s.Reservations[0].Hash = signed.Hash().Hex()
	s.Lanes[0].Hash = signed.Hash().Hex()
	s.Lanes[0].Raw = raw
	s.Lanes[0].Nonce = signed.Nonce()
	r.observation.Receipt.TxHash = signed.Hash()
}

func restoreHistoryFixture(t *testing.T, secondNonce uint64) (metadata.RestoreState, TestnetPolicy, *restoreReaderFixture) {
	t.Helper()
	s, p, r := restoreFixture(t)
	r.transactions = map[common.Hash][]byte{common.HexToHash(s.Jobs[0].Hash): r.raw}
	r.observations = map[common.Hash]ConfirmedObservation{common.HexToHash(s.Jobs[0].Hash): r.observation}
	m := workerManifest(t, "second restore history")
	data, e := registrationData(m)
	if e != nil {
		t.Fatal(e)
	}
	signed, e := types.SignTx(types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(p.ChainID), Nonce: secondNonce, Gas: 50000, GasFeeCap: big.NewInt(20000000000), GasTipCap: big.NewInt(0), To: &p.Contract, Data: data}), types.LatestSignerForChainID(big.NewInt(p.ChainID)), r.signer.key)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := signed.MarshalBinary()
	if e != nil {
		t.Fatal(e)
	}
	hash := signed.Hash().Hex()
	account := s.Accounts[0].Account
	s.Jobs = append(s.Jobs, metadata.RestoreJob{FileID: m.FileID, Target: p.target(), Status: "registered", Hash: hash, Manifest: m})
	s.Reservations = append(s.Reservations, metadata.RestoreReservation{ChainID: p.ChainID, Account: account, Hash: hash, Cost: "1000000000000000"})
	s.Accounts[0].Reserved = "2000000000000000"
	s.Lanes[0] = metadata.RestoreLane{PreparedRegistration: metadata.PreparedRegistration{ChainID: p.ChainID, Account: account, FileID: m.FileID, Target: p.target(), Nonce: secondNonce, Hash: hash, Raw: raw}, State: "confirmed"}
	header := &types.Header{Number: big.NewInt(6)}
	commitment, _ := m.Commitment()
	r.transactions[signed.Hash()] = raw
	r.observations[signed.Hash()] = ConfirmedObservation{Receipt: &types.Receipt{Status: 1, GasUsed: 45000, EffectiveGasPrice: big.NewInt(20000000000), TxHash: signed.Hash(), BlockNumber: big.NewInt(6), BlockHash: header.Hash()}, CanonicalHeader: header, Head: 6, ReadbackBlockHash: header.Hash(), Record: Record{Commitment: commitment, FileSize: uint64(m.Size)}}
	r.latest = secondNonce + 1
	r.pending = r.latest
	return s, p, r
}
func TestRestoreCheckHistoricalSignedTransactions(t *testing.T) {
	s, p, r := restoreHistoryFixture(t, 2)
	if CheckRestoreState(context.Background(), s, p, r) != nil {
		t.Fatal("complete signed history rejected")
	}
	if r.calls != 2 {
		t.Fatal("historical receipt not checked")
	}
	for _, nonce := range []uint64{1, 3} {
		s, p, r = restoreHistoryFixture(t, nonce)
		if CheckRestoreState(context.Background(), s, p, r) == nil {
			t.Fatal("duplicate or missing historical nonce accepted")
		}
	}
	s, p, r = restoreHistoryFixture(t, 2)
	s.Reservations[0].Cost = "900000000000000"
	s.Accounts[0].Reserved = "1900000000000000"
	if CheckRestoreState(context.Background(), s, p, r) == nil {
		t.Fatal("arbitrary historical max reservation accepted")
	}
	s, p, r = restoreHistoryFixture(t, 2)
	s.Lanes[0].Raw = append([]byte(nil), s.Lanes[0].Raw...)
	s.Lanes[0].Raw[len(s.Lanes[0].Raw)-1] ^= 1
	if CheckRestoreState(context.Background(), s, p, r) == nil {
		t.Fatal("latest persisted bytes differing from chain accepted")
	}
	s, p, r = restoreHistoryFixture(t, 2)
	first := common.HexToHash(s.Jobs[0].Hash)
	r.transactions[first] = r.transactions[common.HexToHash(s.Jobs[1].Hash)]
	if CheckRestoreState(context.Background(), s, p, r) == nil {
		t.Fatal("substituted historical transaction accepted")
	}
}
