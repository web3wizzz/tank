package coordinator

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"tank.local/tank/internal/metadata"
	"tank.local/tank/internal/node"
	"tank.local/tank/internal/storage"
)

type uploadFixture struct {
	service  *Service
	store    *metadata.Store
	servers  []*httptest.Server
	backends []*storage.Filesystem
}

func newUploadFixture(t *testing.T, smallFirst bool, failSecondPut bool) uploadFixture {
	t.Helper()
	ctx := context.Background()
	store, err := metadata.Open(ctx, filepath.Join(t.TempDir(), "tank.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	f := uploadFixture{store: store}
	var clients []*node.Client
	token := strings.Repeat("u", 32)
	for i := 0; i < 4; i++ {
		var quota int64
		if i == 0 && smallFirst {
			quota = 1
		}
		backend, err := storage.NewFilesystemWithQuota(t.TempDir(), quota)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { backend.Close() })
		handler, err := node.NewHandler(backend, token)
		if err != nil {
			t.Fatal(err)
		}
		var wrapped http.Handler = handler
		if i == 0 && failSecondPut {
			var puts atomic.Int32
			wrapped = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPut && puts.Add(1) >= 2 {
					http.Error(w, "fixture unavailable", http.StatusServiceUnavailable)
					return
				}
				handler.ServeHTTP(w, r)
			})
		}
		server := httptest.NewServer(wrapped)
		t.Cleanup(server.Close)
		client, err := node.NewClient(server.URL, token)
		if err != nil {
			t.Fatal(err)
		}
		clients = append(clients, client)
		f.servers = append(f.servers, server)
		f.backends = append(f.backends, backend)
	}
	f.service, err = NewService(store, clients, 1024)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func checkUploadPlacement(t *testing.T, m metadata.Manifest, omitted string) {
	t.Helper()
	for _, segment := range m.Segments {
		counts := map[string]int{}
		for _, shard := range segment.Shards {
			if shard.NodeURL == omitted {
				t.Fatal("failed/ineligible node appears in manifest")
			}
			counts[shard.NodeURL]++
		}
		if len(counts) != 3 {
			t.Fatalf("placement uses %d nodes, want 3", len(counts))
		}
		for _, count := range counts {
			if count != 2 {
				t.Fatal("shards concentrated in one node")
			}
		}
	}
}

func TestNewUploadUsesSpareAfterOriginalNodeFailure(t *testing.T) {
	f := newUploadFixture(t, false, false)
	ctx := context.Background()
	f.servers[0].Close()
	data := bytes.Repeat([]byte("new encrypted file bytes"), 150)
	m, err := f.service.Tank(ctx, data)
	if err != nil {
		t.Fatal(err)
	}
	checkUploadPlacement(t, m, f.servers[0].URL)
	got, err := f.service.Retrieve(ctx, m.FileID)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("retrieve: %v", err)
	}
	// The new placement still tolerates loss of any one of its three nodes.
	f.servers[1].Close()
	got, err = f.service.Retrieve(ctx, m.FileID)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("new placement recovery: %v", err)
	}
}

func TestUploadSkipsNodeWithoutWholeFileHeadroom(t *testing.T) {
	f := newUploadFixture(t, true, false)
	m, err := f.service.Tank(context.Background(), bytes.Repeat([]byte("x"), 2049))
	if err != nil {
		t.Fatal(err)
	}
	checkUploadPlacement(t, m, f.servers[0].URL)
	status, err := f.backends[0].Status(context.Background())
	if err != nil || status.UsedFiles != 0 {
		t.Fatal("capacity rejection wrote shards")
	}
}

func TestUploadRetriesWholeSlotWithoutConcentratingShards(t *testing.T) {
	f := newUploadFixture(t, false, true)
	data := bytes.Repeat([]byte("x"), 800)
	m, err := f.service.Tank(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	checkUploadPlacement(t, m, f.servers[0].URL)
	got, err := f.service.Retrieve(context.Background(), m.FileID)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("retrieve after PUT failure: %v", err)
	}
}

func TestUploadRefusesInsufficientDistinctNodesBeforeWrites(t *testing.T) {
	f := newUploadFixture(t, false, false)
	f.servers[0].Close()
	f.servers[1].Close()
	_, err := f.service.Tank(context.Background(), []byte("unavailable"))
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want unavailable, got %v", err)
	}
	for _, backend := range f.backends {
		status, err := backend.Status(context.Background())
		if err != nil || status.UsedFiles != 0 {
			t.Fatal("rejected upload retained shards")
		}
	}
	list, err := f.store.List(context.Background(), "", 100)
	if err != nil || len(list) != 0 {
		t.Fatal("rejected upload published a manifest")
	}
}

func TestConcurrentNewUploadsUseHealthyPlacement(t *testing.T) {
	f := newUploadFixture(t, false, false)
	f.servers[0].Close()
	var workers sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			data := bytes.Repeat([]byte{byte(index + 1)}, 2051)
			m, err := f.service.Tank(context.Background(), data)
			if err == nil {
				var restored []byte
				restored, err = f.service.Retrieve(context.Background(), m.FileID)
				if err == nil && !bytes.Equal(restored, data) {
					err = errors.New("bytes differ")
				}
			}
			results <- err
		}(i)
	}
	workers.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
}
