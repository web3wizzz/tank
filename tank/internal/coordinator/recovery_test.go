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

func TestRetrieveAfterNodeFailure(t *testing.T) {
	ctx := context.Background()
	token := strings.Repeat("x", 32)
	var clients []*node.Client
	var servers []*httptest.Server

	for i := 0; i < 3; i++ {
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

	data := bytes.Repeat([]byte("Tank exact recovery."), 200)
	m, err := service.Tank(ctx, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Segments) < 2 {
		t.Fatal("expected multiple segments")
	}

	servers[0].Close()

	got, err := service.Retrieve(ctx, m.FileID)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("retrieval after node failure: %v", err)
	}

	// Reopen metadata and create a new coordinator service.
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

	got, err = restarted.Retrieve(ctx, m.FileID)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("retrieval after coordinator restart: %v", err)
	}

	servers[1].Close()
	if _, err := restarted.Retrieve(ctx, m.FileID); err == nil {
		t.Fatal("expected failure after losing two nodes")
	}
}
