package coordinator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"tank.local/tank/internal/encoding"
	"tank.local/tank/internal/integrity"
	"tank.local/tank/internal/metadata"
	"tank.local/tank/internal/node"
	"tank.local/tank/internal/storage"
)

func (s *Service) uploadCandidates(ctx context.Context, size int) ([]*node.Client, error) {
	// Each of three distinct failure domains gets two shards per segment. Reserve enough
	// observed headroom for one complete placement slot across the entire file.
	var requiredBytes int64
	segments := 0
	for remaining := size; remaining > 0; {
		part := min(remaining, s.segmentSize)
		requiredBytes += 2 * int64((part+encoding.DataShards-1)/encoding.DataShards)
		segments++
		remaining -= part
	}
	probe, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	eligible := make([]bool, len(s.placementNodes))
	responded := make([]bool, len(s.placementNodes))
	jobs := make(chan int, len(eligible))
	for i := range eligible {
		jobs <- i
	}
	close(jobs)
	var workers sync.WaitGroup
	for i := 0; i < min(4, len(eligible)); i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				status, err := s.placementNodes[index].Status(probe)
				responded[index] = err == nil
				eligible[index] = err == nil && !status.AtCapacity &&
					(status.ByteLimit == 0 || status.ByteLimit-status.UsedBytes >= requiredBytes) &&
					(status.FileLimit == 0 || status.FileLimit-status.UsedFiles >= 2*segments)
			}
		}()
	}
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	candidates := make([]*node.Client, 0, len(eligible))
	for i, ok := range eligible {
		if ok {
			candidates = append(candidates, s.placementNodes[i])
		}
	}
	domains := map[string]bool{}
	for _, client := range candidates {
		domains[client.FailureDomain()] = true
	}
	if len(domains) < 3 {
		availableDomains := map[string]bool{}
		for i, ok := range responded {
			if ok {
				availableDomains[s.placementNodes[i].FailureDomain()] = true
			}
		}
		if len(availableDomains) >= 3 {
			return nil, fmt.Errorf("insufficient upload capacity: %w", storage.ErrQuotaExceeded)
		}
		return nil, fmt.Errorf("three available failure domains with upload capacity required: %w", ErrUnavailable)
	}
	return candidates, nil
}

func placeUploadSegment(ctx context.Context, id string, segment *metadata.Segment, shards [][]byte, candidates []*node.Client) error {
	used := make(map[string]bool, 3)
	attempted := map[string]bool{}
	for slot := 0; slot < 3; slot++ {
		placed := false
		for _, client := range candidates {
			if attempted[client.BaseURL] || used[client.FailureDomain()] {
				continue
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			// Treat a failed/ambiguous candidate as unavailable for the remainder
			// of this segment. Both slot shards must be accepted by one node.
			attempted[client.BaseURL] = true
			ok := true
			for index := slot; index < len(shards); index += 3 {
				hash := integrity.Digest(shards[index])
				key := storage.ShardKey{FileID: id, Segment: segment.Index, Index: index}
				if err := client.Put(ctx, key, shards[index], hash); err != nil {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
			for index := slot; index < len(shards); index += 3 {
				segment.Shards[index] = metadata.Shard{Index: index,
					Hash: integrity.Digest(shards[index]).String(), NodeURL: client.BaseURL}
			}
			used[client.FailureDomain()] = true
			placed = true
			break
		}
		if !placed {
			return fmt.Errorf("store segment %d without concentrating shards: %w", segment.Index, ErrUnavailable)
		}
	}
	return nil
}
