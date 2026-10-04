package registry

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"tank.local/tank/internal/integrity"
	"tank.local/tank/internal/metadata"
)

type Worker struct {
	store  *metadata.Store
	config Config
	target string
}

func NewWorker(store *metadata.Store, cfg Config) (*Worker, error) {
	if store == nil {
		return nil, errors.New("registration worker requires a metadata store")
	}

	u, err := url.Parse(cfg.RPCURL)
	if err != nil || u == nil {
		return nil, errors.New("invalid chain RPC URL")
	}
	if (u.Scheme != "http" && u.Scheme != "https") ||
		u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("invalid chain RPC URL")
	}

	host := u.Hostname()
	ip := net.ParseIP(host)
	if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
		return nil, errors.New("registration worker currently supports local Anvil only")
	}

	if !common.IsHexAddress(cfg.Contract) ||
		common.HexToAddress(cfg.Contract) == (common.Address{}) {
		return nil, errors.New("invalid registry address")
	}
	if !common.IsHexAddress(cfg.Registrant) ||
		common.HexToAddress(cfg.Registrant) == (common.Address{}) {
		return nil, errors.New("invalid registrant address")
	}

	cfg.Contract = common.HexToAddress(cfg.Contract).Hex()
	cfg.Registrant = common.HexToAddress(cfg.Registrant).Hex()

	return &Worker{
		store:  store,
		config: cfg,
		target: "31337:" + strings.ToLower(cfg.Contract) +
			":" + strings.ToLower(cfg.Registrant),
	}, nil
}

// Reconcile closes the gap between saving a manifest and enqueueing its
// registration. Completed jobs remain completed when encountered again.
func (w *Worker) Reconcile(ctx context.Context) error {
	after := ""
	for {
		ids, err := w.store.List(ctx, after, 100)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if err := w.store.EnqueueRegistration(ctx, id, w.target); err != nil {
				return err
			}
		}
		if len(ids) < 100 {
			return nil
		}
		after = ids[len(ids)-1]
	}
}

func (w *Worker) Process(ctx context.Context) (bool, error) {
	job, err := w.store.ClaimRegistration(ctx, w.target, 3*time.Minute)
	if errors.Is(err, metadata.ErrNoRegistrationJob) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	workCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	permanent, workErr := w.register(workCtx, job)
	if ctx.Err() != nil {
		// Keep the lease and transaction hash for recovery after shutdown.
		return true, ctx.Err()
	}

	finishCtx, finishCancel := context.WithTimeout(ctx, 5*time.Second)
	defer finishCancel()

	if workErr == nil {
		return true, w.store.CompleteRegistration(finishCtx, job)
	}
	if permanent {
		return true, w.store.FailRegistration(finishCtx, job, workErr.Error())
	}

	delay := 30 * time.Second
	for attempt := 1; attempt < job.Attempts && delay < 15*time.Minute; attempt++ {
		delay *= 2
	}
	if delay > 15*time.Minute {
		delay = 15 * time.Minute
	}
	if err := w.store.RetryRegistration(
		finishCtx, job, delay, workErr.Error(),
	); err != nil {
		return true, err
	}

	log.Printf("registration %s will retry: %v", job.FileID, workErr)
	return true, nil
}

func (w *Worker) register(
	ctx context.Context, job metadata.RegistrationJob,
) (bool, error) {
	manifest, err := w.store.Load(ctx, job.FileID)
	if err != nil {
		return false, err
	}
	commitment, err := manifest.Commitment()
	if err != nil {
		return true, err
	}
	id, err := integrity.ParseHash(job.FileID)
	if err != nil {
		return true, err
	}

	// Connect here, rather than at startup, so an unavailable chain can retry.
	client, err := Open(ctx, w.config)
	if err != nil {
		return false, err
	}
	defer client.Close()

	record, found, err := client.Get(ctx, id)
	if err != nil {
		return false, err
	}
	if found {
		if record.Commitment != commitment || record.FileSize != uint64(manifest.Size) {
			return true, errors.New("existing registry record conflicts with manifest")
		}
		return false, nil
	}

	var txHash common.Hash
	if job.TransactionHash != "" {
		txHash = common.HexToHash(job.TransactionHash)
	} else {
		txHash, err = client.Register(ctx, manifest)
		if err != nil {
			return false, err
		}
		if txHash != (common.Hash{}) {
			if err := w.store.SaveRegistrationTransaction(
				ctx, job, txHash.Hex(),
			); err != nil {
				return false, err
			}
		}
	}

	if err := client.Wait(ctx, txHash); err != nil {
		if ctx.Err() == nil && txHash != (common.Hash{}) {
			receipt, receiptErr := client.chain.TransactionReceipt(ctx, txHash)
			if receiptErr == nil && receipt.Status == types.ReceiptStatusFailed {
				return true, fmt.Errorf("registration transaction %s reverted", txHash.Hex())
			}
		}
		return false, err
	}

	record, found, err = client.Get(ctx, id)
	if err != nil {
		return false, err
	}
	if !found {
		return false, errors.New("registry record missing after transaction")
	}
	if record.Commitment != commitment || record.FileSize != uint64(manifest.Size) {
		return true, errors.New("registry readback conflicts with manifest")
	}
	return false, nil
}

func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var nextReconcile time.Time
	for {
		if ctx.Err() != nil {
			return
		}

		if !time.Now().Before(nextReconcile) {
			reconcileCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			err := w.Reconcile(reconcileCtx)
			cancel()
			if err != nil && ctx.Err() == nil {
				log.Printf("registration reconciliation: %v", err)
			}
			nextReconcile = time.Now().Add(30 * time.Second)
		}

		processed, err := w.Process(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("registration worker: %v", err)
		}
		if processed && err == nil {
			continue
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
