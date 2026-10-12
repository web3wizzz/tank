package registry

import (
	"context"
	"errors"
	"github.com/ethereum/go-ethereum/common"
	"tank.local/tank/internal/integrity"
	"tank.local/tank/internal/metadata"
	"testing"
	"time"
)

type preparationFixture struct {
	calls   int
	failure bool
}

func (r *preparationFixture) Terms(context.Context, metadata.Manifest, TestnetPolicy) (TransactionTerms, error) {
	r.calls++
	if r.failure {
		return TransactionTerms{}, errors.New("synthetic-provider-secret-do-not-echo")
	}
	return termsFixture(), nil
}
func (r *preparationFixture) Observation(context.Context, common.Hash, integrity.Hash) (ConfirmedObservation, error) {
	return ConfirmedObservation{}, ErrConfirmation
}

func TestSignedWorkerRetryDoesNotSignOrEstimateAgain(t *testing.T) {
	ctx := preparationContext(t)
	store := workerStore(t)
	signer := signerFixture(t)
	p := policyFixture(signer)
	p.Confirmations = 1
	reader := &preparationFixture{}
	w, err := NewSignedWorker(store, p, signer, reader)
	if err != nil {
		t.Fatal(err)
	}
	w.retryDelay = time.Second
	m := workerManifest(t, "non-broadcast worker")
	if err := store.Save(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := w.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if worked, err := w.Process(ctx); err != nil || !worked {
			t.Fatalf("worker did not preserve retry: %v", err)
		}
		if i == 0 {
			time.Sleep(1100 * time.Millisecond)
		}
	}
	status, err := store.GetRegistrationStatus(ctx, m.FileID, w.Target())
	if err != nil || status.TransactionHash == "" || status.State == "registered" || signer.calls.Load() != 1 || reader.calls != 1 {
		t.Fatal("retry changed intent or falsely confirmed")
	}
	// A second registry shares the same account nonce lane, but has a separate job.
	p.Contract = common.HexToAddress("0x3333333333333333333333333333333333333333")
	other, err := NewSignedWorker(store, p, signer, reader)
	if err != nil {
		t.Fatal(err)
	}
	other.retryDelay = time.Second
	if err := other.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if worked, err := other.Process(ctx); err != nil || !worked {
		t.Fatal("conflicting lane retry failed")
	}
	if signer.calls.Load() != 1 || reader.calls != 1 {
		t.Fatal("busy signer lane reached custody")
	}
}
