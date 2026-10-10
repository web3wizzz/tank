package coordinator

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"tank.local/tank/internal/encoding"
	"tank.local/tank/internal/integrity"
	"tank.local/tank/internal/metadata"
	"tank.local/tank/internal/storage"
)

// AuditFile checks every assigned shard against its manifest hash.
func (s *Service) AuditFile(ctx context.Context, id string) (bool, error) {
	m, err := s.store.Load(ctx, id)
	if err != nil {
		return false, err
	}

	for _, segment := range m.Segments {
		expectedSize := (segment.Size + encoding.DataShards - 1) /
			encoding.DataShards

		for _, record := range segment.Shards {
			if err := ctx.Err(); err != nil {
				return false, err
			}

			client, exists := s.byURL[record.NodeURL]
			if !exists {
				return false, nil
			}

			hash, err := integrity.ParseHash(record.Hash)
			if err != nil {
				return false, err
			}

			key := storage.ShardKey{
				FileID: id, Segment: segment.Index, Index: record.Index,
			}
			data, err := client.Get(ctx, key, hash)

			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			if err != nil || len(data) != expectedSize {
				return false, nil
			}
		}
	}

	return true, nil
}

// AuditOnce checks stored files and queues unhealthy ones.
// The worker checks them again before repairing.
func (s *Service) AuditOnce(ctx context.Context) (result error) {
	defer func() {
		state := "completed"
		if result != nil {
			state = "failed"
		}
		s.auditStatus.Store(&AuditStatus{State: state, CompletedAt: time.Now().UTC()})
	}()
	after := ""

	for {
		ids, err := s.store.List(ctx, after, 100)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}

		for _, id := range ids {
			healthy, err := s.AuditFile(ctx, id)
			if err != nil {
				return err
			}
			if !healthy {
				if err := s.store.EnqueueRepair(ctx, id); err != nil {
					return err
				}
				log.Printf("[Tank] audit queued repair for %s", id)
			}
		}

		after = ids[len(ids)-1]
	}
}

func (s *Service) repairUsingSpare(ctx context.Context, id string) (int, error) {
	healthy, err := s.AuditFile(ctx, id)
	if err != nil {
		return 0, err
	}
	if healthy {
		return 0, nil
	}

	m, err := s.store.Load(ctx, id)
	if err != nil {
		return 0, err
	}

	used := make(map[string]bool)
	for _, segment := range m.Segments {
		for _, shard := range segment.Shards {
			used[shard.NodeURL] = true
		}
	}

	var candidates []string
	for address := range s.byURL {
		if !used[address] {
			candidates = append(candidates, address)
		}
	}
	sort.Strings(candidates)

	if len(candidates) == 0 {
		return 0, fmt.Errorf("no unused replacement node is configured")
	}

	var lastError error
	for _, address := range candidates {
		if err := ctx.Err(); err != nil {
			return 0, err
		}

		count, err := s.Repair(ctx, id, address)
		if err == nil {
			return count, nil
		}

		lastError = err
	}

	return 0, fmt.Errorf("replacement attempts failed: %w", lastError)
}

// ProcessRepairJob handles at most one job.
// Work times out before its lease expires.
func (s *Service) ProcessRepairJob(ctx context.Context) (bool, error) {
	job, err := s.store.ClaimRepair(ctx, 3*time.Minute)
	if errors.Is(err, metadata.ErrNoRepairJob) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	workCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	count, repairErr := s.repairUsingSpare(workCtx, job.FileID)
	cancel()

	// Leave the lease to expire if the coordinator is shutting down.
	if ctx.Err() != nil {
		return true, ctx.Err()
	}

	finishCtx, finishCancel := context.WithTimeout(ctx, 5*time.Second)
	defer finishCancel()

	if repairErr == nil {
		if err := s.store.CompleteRepair(finishCtx, job); err != nil {
			return true, err
		}

		log.Printf(
			"[Tank] automatic repair completed: %s | %d shards",
			job.FileID, count,
		)
		return true, nil
	}

	// Retry after 30 seconds, increasing to a maximum of 15 minutes.
	exponent := min(max(job.Attempts-1, 0), 5)
	delay := min(
		30*time.Second*time.Duration(1<<exponent),
		15*time.Minute,
	)

	if err := s.store.RetryRepair(
		finishCtx, job, delay, repairErr.Error(),
	); err != nil {
		return true, err
	}

	log.Printf(
		"[Tank] repair retry scheduled: %s | delay=%s | %v",
		job.FileID, delay, repairErr,
	)

	return true, nil
}

// RunMaintenance blocks until both background loops stop.
func (s *Service) RunMaintenance(ctx context.Context) {
	var workers sync.WaitGroup
	workers.Add(2)

	go func() {
		defer workers.Done()

		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			auditCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			err := s.AuditOnce(auditCtx)
			cancel()

			if err != nil && ctx.Err() == nil {
				log.Printf("[Tank] audit error: %v", err)
			}

			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()

	go func() {
		defer workers.Done()

		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			_, err := s.ProcessRepairJob(ctx)
			if err != nil && ctx.Err() == nil {
				log.Printf("[Tank] repair worker error: %v", err)
			}

			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()

	workers.Wait()
}
