package coordinator

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"tank.local/tank/internal/metadata"
	"tank.local/tank/internal/storage"
	"time"
)

type AuditStatus struct {
	State       string    `json:"state"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
}
type nodeStatus struct {
	Index            int             `json:"index"`
	InitialPlacement bool            `json:"initial_placement"`
	Available        bool            `json:"available"`
	Capacity         *storage.Status `json:"capacity,omitempty"`
}

func (s *Service) operationsHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	metadataStatus, err := s.store.Operations(ctx)
	if err != nil {
		http.Error(w, "operational metadata unavailable", http.StatusServiceUnavailable)
		return
	}
	addresses := make([]string, 0, len(s.byURL))
	for address := range s.byURL {
		addresses = append(addresses, address)
	}
	sort.Strings(addresses)
	initial := map[string]bool{}
	for _, client := range s.nodes {
		initial[client.BaseURL] = true
	}
	results := make([]nodeStatus, len(addresses))
	jobs := make(chan int, len(addresses))
	for i := range addresses {
		jobs <- i
	}
	close(jobs)
	var workers sync.WaitGroup
	for n := 0; n < min(4, len(addresses)); n++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				result := nodeStatus{Index: i + 1, InitialPlacement: initial[addresses[i]]}
				capacity, err := s.byURL[addresses[i]].Status(ctx)
				if err == nil {
					result.Available = true
					result.Capacity = &capacity
				}
				results[i] = result
			}
		}()
	}
	workers.Wait()
	ready := true
	degraded := metadataStatus.RepairJobs > 0
	for _, result := range results {
		if !result.Available {
			degraded = true
		}
		if result.InitialPlacement && (!result.Available || result.Capacity.AtCapacity) {
			ready = false
		}
	}
	if s.resourceLimits.TotalStorageBytes > 0 && metadataStatus.CommittedBytes >= s.resourceLimits.TotalStorageBytes {
		ready = false
	}
	audit := AuditStatus{State: "never"}
	if last := s.auditStatus.Load(); last != nil {
		audit = *last
	}
	response := struct {
		UploadReady  bool                `json:"upload_ready"`
		Degraded     bool                `json:"degraded"`
		Metadata     metadata.Operations `json:"metadata"`
		LogicalLimit int64               `json:"logical_byte_limit"`
		Nodes        []nodeStatus        `json:"nodes"`
		Audit        AuditStatus         `json:"last_audit"`
	}{ready, degraded, metadataStatus, s.resourceLimits.TotalStorageBytes, results, audit}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
