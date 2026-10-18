package coordinator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
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
	configureUploadDomains(t, f, "host-a", "host-b", "host-c", "host-d")
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

func TestUploadAndReadinessRejectUnknownNodeCapacity(t *testing.T) {
	for _, invalidNodes := range []int{1, 2} {
		t.Run(fmt.Sprintf("invalid-nodes-%d", invalidNodes), func(t *testing.T) {
			f := newUploadFixture(t, false, false)
			for i := 0; i < invalidNodes; i++ {
				original := f.servers[i].Config.Handler
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/ops/status" {
						w.Write([]byte(`{"used_bytes":0,"byte_limit":null,"used_files":0,"file_limit":0}`))
						return
					}
					original.ServeHTTP(w, r)
				}))
				t.Cleanup(server.Close)
				client, err := node.NewClient(server.URL, strings.Repeat("u", 32))
				if err != nil {
					t.Fatal(err)
				}
				delete(f.service.byURL, f.servers[i].URL)
				f.service.byURL[client.BaseURL] = client
				f.service.nodes[i] = client
				f.service.placementNodes[i] = client
				f.servers[i] = server
			}
			configureUploadDomains(t, f, "host-a", "host-b", "host-c", "host-d")
			response := httptest.NewRecorder()
			f.service.operationsHandler(response, httptest.NewRequest(http.MethodGet, "/ops/status", nil))
			var status struct {
				UploadReady bool `json:"upload_ready"`
				Degraded    bool `json:"degraded"`
			}
			if response.Code != http.StatusOK {
				t.Fatal("operations unavailable")
			}
			if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
				t.Fatal(err)
			}
			if !status.Degraded || status.UploadReady != (invalidNodes == 1) {
				t.Fatal("unknown capacity counted toward readiness")
			}
			data := bytes.Repeat([]byte("eligible healthy spare"), 100)
			m, err := f.service.Tank(context.Background(), data)
			if invalidNodes == 1 {
				if err != nil {
					t.Fatal(err)
				}
				checkUploadPlacement(t, m, f.servers[0].URL)
				got, err := f.service.Retrieve(context.Background(), m.FileID)
				if err != nil || !bytes.Equal(got, data) {
					t.Fatal("spare retrieval failed", err)
				}
			} else {
				if !errors.Is(err, ErrUnavailable) {
					t.Fatal("unknown capacity admitted", err)
				}
				ids, err := f.store.List(context.Background(), "", 100)
				if err != nil || len(ids) != 0 {
					t.Fatal("failed admission published metadata", err)
				}
			}
			for i, backend := range f.backends {
				if i >= invalidNodes && invalidNodes == 1 {
					continue
				}
				capacity, err := backend.Status(context.Background())
				if err != nil || capacity.UsedFiles != 0 {
					t.Fatal("ineligible admission wrote shards", err)
				}
			}
		})
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
	configureUploadDomains(t, f, "host-a", "host-b", "host-c", "host-d")
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

func configureUploadDomains(t *testing.T, f uploadFixture, labels ...string) {
	t.Helper()
	mapping := map[string]string{}
	for i, client := range f.service.placementNodes {
		mapping[client.BaseURL] = labels[i]
	}
	body, err := json.Marshal(mapping)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "domains.json")
	if err = os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	if err = node.ConfigureFailureDomains(f.service.placementNodes, path, true); err != nil {
		t.Fatal(err)
	}
}

func TestUploadRequiresHealthyDistinctFailureDomains(t *testing.T) {
	f := newUploadFixture(t, false, false)
	configureUploadDomains(t, f, "same-host", "same-host", "second-host", "same-host")
	if _, err := f.service.Tank(context.Background(), []byte("must not concentrate")); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("shared domains accepted: %v", err)
	}
	for _, backend := range f.backends {
		status, err := backend.Status(context.Background())
		if err != nil || status.UsedFiles != 0 {
			t.Fatal("rejected placement wrote shards")
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/ops/status", nil)
	response := httptest.NewRecorder()
	f.service.operationsHandler(response, request)
	var status struct {
		UploadReady bool `json:"upload_ready"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil || status.UploadReady {
		t.Fatal("readiness counted endpoints rather than domains", err)
	}
}

func TestUploadRetriesAlternateInFailedCandidateDomain(t *testing.T) {
	f := newUploadFixture(t, false, true)
	configureUploadDomains(t, f, "domain-a", "domain-b", "domain-c", "domain-a")
	data := bytes.Repeat([]byte("retry domain"), 70)
	m, err := f.service.Tank(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	checkUploadPlacement(t, m, f.servers[0].URL)
	for _, segment := range m.Segments {
		counts := map[string]int{}
		for _, shard := range segment.Shards {
			counts[f.service.byURL[shard.NodeURL].FailureDomain()]++
		}
		if len(counts) != 3 {
			t.Fatal("placement collapsed domains")
		}
		for _, count := range counts {
			if count != 2 {
				t.Fatal("more than two shards in domain")
			}
		}
	}
	got, err := f.service.Retrieve(context.Background(), m.FileID)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("retry changed bytes", err)
	}
}

func TestRepairRejectsDomainSharedWithSurvivingShards(t *testing.T) {
	f := newUploadFixture(t, false, false)
	configureUploadDomains(t, f, "domain-a", "domain-b", "domain-c", "domain-b")
	m, err := f.service.Tank(context.Background(), bytes.Repeat([]byte("repair"), 100))
	if err != nil {
		t.Fatal(err)
	}
	f.servers[0].Close()
	if _, err = f.service.Repair(context.Background(), m.FileID, f.servers[3].URL); err == nil {
		t.Fatal("repair concentrated surviving failure domain")
	}
	if _, err = f.service.repairUsingSpare(context.Background(), m.FileID); err == nil {
		t.Fatal("maintenance concentrated surviving failure domain")
	}
	after, err := f.store.Load(context.Background(), m.FileID)
	if err != nil {
		t.Fatal(err)
	}
	beforeJSON, _ := json.Marshal(m)
	afterJSON, _ := json.Marshal(after)
	if !bytes.Equal(beforeJSON, afterJSON) {
		t.Fatal("rejected repair modified manifest")
	}
	status, err := f.backends[3].Status(context.Background())
	if err != nil || status.UsedFiles != 0 {
		t.Fatal("rejected repair wrote replacement shards")
	}
	// Replacing the lost domain by another node in that domain is safe.
	configureUploadDomains(t, f, "domain-a", "domain-b", "domain-c", "domain-a")
	if count, err := f.service.Repair(context.Background(), m.FileID, f.servers[3].URL); err != nil || count != 2 {
		t.Fatalf("safe same-lost-domain replacement: %d, %v", count, err)
	}
}
