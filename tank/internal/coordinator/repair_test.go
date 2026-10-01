package coordinator

import (
	"bytes"
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"tank.local/tank/internal/metadata"
	"tank.local/tank/internal/node"
	"tank.local/tank/internal/storage"
)

func TestRepairRestoresRedundancy(t *testing.T) {
	ctx := context.Background()
	token := strings.Repeat("r", 32)

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

	dbPath := filepath.Join(t.TempDir(), "tank.sqlite")
	store, err := metadata.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	service, err := NewService(store, clients, 1024)
	if err != nil {
		t.Fatal(err)
	}

	data := bytes.Repeat([]byte("Tank repairs lost shards."), 150)
	manifest, err := service.Tank(ctx, data)
	if err != nil {
		t.Fatal(err)
	}

	// Node one goes offline, losing two shards per segment.
	servers[0].Close()

	repaired, err := service.Repair(ctx, manifest.FileID, clients[3].BaseURL)
	if err != nil {
		t.Fatal(err)
	}

	expected := len(manifest.Segments) * 2
	if repaired != expected {
		t.Fatalf("expected %d repaired shards, got %d", expected, repaired)
	}

	// Repeating a completed repair should find nothing missing.
	again, err := service.Repair(ctx, manifest.FileID, clients[3].BaseURL)
	if err != nil || again != 0 {
		t.Fatalf("repeated repair: count=%d error=%v", again, err)
	}

	// Lose another original node after repair. The replacement and
	// remaining original node must still reconstruct the entire file.
	servers[1].Close()

	got, err := service.Retrieve(ctx, manifest.FileID)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("retrieval after second node failure: %v", err)
	}

	// Replacement assignments must survive a coordinator restart.
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := metadata.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	restarted, err := NewService(reopened, clients, 1024)
	if err != nil {
		t.Fatal(err)
	}

	got, err = restarted.Retrieve(ctx, manifest.FileID)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("retrieval after restart: %v", err)
	}
}
