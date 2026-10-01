package coordinator

import (
	"bytes"
	"context"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tank.local/tank/internal/metadata"
	"tank.local/tank/internal/node"
	"tank.local/tank/internal/storage"
)

func TestAuditQueuesAndRepairsNodeLoss(t *testing.T) {
	ctx := context.Background()
	token := strings.Repeat("a", 32)

	var clients []*node.Client
	var servers []*httptest.Server

	for i := 0; i < 4; i++ {
		backend, err := storage.NewFilesystem(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer backend.Close()

		handler, err := node.NewHandler(backend, token)
		if err != nil {
			t.Fatal(err)
		}

		server := httptest.NewServer(handler)
		defer server.Close()
		servers = append(servers, server)

		client, err := node.NewClient(server.URL, token)
		if err != nil {
			t.Fatal(err)
		}
		clients = append(clients, client)
	}

	store, err := metadata.Open(
		ctx, filepath.Join(t.TempDir(), "tank.sqlite"),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	service, err := NewService(store, clients, 1024)
	if err != nil {
		t.Fatal(err)
	}

	data := bytes.Repeat([]byte("Tank automatic repair."), 100)
	m, err := service.Tank(ctx, data)
	if err != nil {
		t.Fatal(err)
	}

	servers[0].Close()

	if err := service.AuditOnce(ctx); err != nil {
		t.Fatal(err)
	}

	processed, err := service.ProcessRepairJob(ctx)
	if err != nil || !processed {
		t.Fatalf("repair job was not processed: %v", err)
	}

	healthy, err := service.AuditFile(ctx, m.FileID)
	if err != nil || !healthy {
		t.Fatalf("file remains unhealthy after repair: %v", err)
	}

	if _, err := store.ClaimRepair(ctx, time.Minute); !errors.Is(err, metadata.ErrNoRepairJob) {
		t.Fatalf("completed job remained queued: %v", err)
	}

	// The restored redundancy must tolerate another node failure.
	servers[1].Close()

	got, err := service.Retrieve(ctx, m.FileID)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("retrieval after automatic repair failed: %v", err)
	}
}
