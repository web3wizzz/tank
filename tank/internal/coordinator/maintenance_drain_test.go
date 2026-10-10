package coordinator

import (
	"context"
	"fmt"
	"tank.local/tank/internal/limits"
	"testing"
	"time"
)

func TestMaintenanceDrainsReadyJobsWithoutPerFileTickerDelay(t *testing.T) {
	fixture := newResourceFixture(t, limits.Default())
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		manifest, err := fixture.service.Tank(ctx, []byte(fmt.Sprintf("ready repair fixture %d", i)))
		if err != nil {
			t.Fatal(err)
		}
		if err := fixture.store.EnqueueRepair(ctx, manifest.FileID); err != nil {
			t.Fatal(err)
		}
	}
	work, cancel := context.WithTimeout(ctx, 2*time.Second)
	done := make(chan struct{})
	go func() { defer close(done); fixture.service.RunMaintenance(work) }()
	defer func() { cancel(); <-done }()
	for {
		status, err := fixture.store.Operations(work)
		if err != nil {
			t.Fatal("ready repair backlog was not drained before deadline")
		}
		if status.RepairJobs == 0 {
			break
		}
		select {
		case <-work.Done():
			t.Fatal("maintenance added a ticker delay per ready file")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
