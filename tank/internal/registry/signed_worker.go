package registry

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"tank.local/tank/internal/integrity"
	"tank.local/tank/internal/metadata"
)

// PreparationReader deliberately has no send/sign method. A future authorized
// broadcaster must reuse persisted bytes, not allocate another nonce.
type PreparationReader interface {
	Terms(context.Context, metadata.Manifest, TestnetPolicy) (TransactionTerms, error)
	Observation(context.Context, common.Hash, integrity.Hash) (ConfirmedObservation, error)
}

// SignedWorker prepares and reconciles intents, but cannot broadcast. It is
// separate from the local unlocked worker, whose RPC restrictions stay intact.
type SignedWorker struct {
	store      *metadata.Store
	policy     TestnetPolicy
	signer     TransactionSigner
	reader     PreparationReader
	retryDelay time.Duration
}

func NewSignedWorker(store *metadata.Store, p TestnetPolicy, signer TransactionSigner, reader PreparationReader) (*SignedWorker, error) {
	if store == nil || !p.valid() || p.ChainID != ArcTestnetChainID || p.Confirmations != 1 || signer == nil || signer.Address() != p.Registrant || reader == nil {
		return nil, ErrPreparation
	}
	// Copy caller-owned integer bounds so they cannot change while processing.
	p.MaxFeeWei = new(big.Int).Set(p.MaxFeeWei)
	p.MaxTipWei = new(big.Int).Set(p.MaxTipWei)
	p.MaxExecutionCostWei = new(big.Int).Set(p.MaxExecutionCostWei)
	return &SignedWorker{store: store, policy: p, signer: signer, reader: reader, retryDelay: 30 * time.Second}, nil
}

func (w *SignedWorker) Target() string { return w.policy.target() }
func (w *SignedWorker) Reconcile(ctx context.Context) error {
	return (&Worker{store: w.store, target: w.Target()}).Reconcile(ctx)
}

func (w *SignedWorker) Process(ctx context.Context) (bool, error) {
	job, err := w.store.ClaimRegistration(ctx, w.Target(), 3*time.Minute)
	if errors.Is(err, metadata.ErrNoRegistrationJob) {
		return false, nil
	}
	if err != nil {
		return false, journalError(err)
	}
	work, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	err = w.processJob(work, job)
	if ctx.Err() != nil {
		return true, ctx.Err()
	}
	if err == nil {
		return true, nil
	} // confirmation atomically completed job
	// Failure, absent receipts and ambiguous responses all preserve the lane.
	// Do not release/re-sign a transaction on a fee-floor or blocklist failure.
	finish, done := context.WithTimeout(ctx, 5*time.Second)
	defer done()
	delay := w.retryDelay
	for i := 1; i < job.Attempts && delay < 15*time.Minute; i++ {
		delay *= 2
	}
	if delay > 15*time.Minute {
		delay = 15 * time.Minute
	}
	if err := w.store.RetryRegistration(finish, job, delay, "signed intent awaiting canonical receipt/readback or operator review"); err != nil {
		return true, journalError(err)
	}
	return true, nil
}

func (w *SignedWorker) processJob(ctx context.Context, job metadata.RegistrationJob) error {
	m, err := w.store.Load(ctx, job.FileID)
	if err != nil {
		return ErrPreparation
	}
	data, err := registrationData(m)
	if err != nil {
		return err
	}
	value, err := w.store.PreparedRegistration(ctx, job, w.policy.ChainID, strings.ToLower(w.policy.Registrant.Hex()))
	if errors.Is(err, metadata.ErrNotFound) {
		terms, err := w.reader.Terms(ctx, m, w.policy)
		if err != nil {
			return ErrTestnetRPC
		}
		value, err = PrepareTestnetRegistration(ctx, w.store, job, w.policy, w.signer, terms)
		if err != nil {
			return err
		}
	} else if err != nil {
		return journalError(err)
	}
	if _, err := decodePrepared(value, w.policy, data); err != nil {
		return err
	}
	id, err := integrity.ParseHash(m.FileID)
	if err != nil {
		return ErrPreparation
	}
	observation, err := w.reader.Observation(ctx, common.HexToHash(value.Hash), id)
	if err != nil {
		return ErrConfirmation
	}
	return ConfirmPreparedRegistration(ctx, w.store, job, w.policy, observation)
}
