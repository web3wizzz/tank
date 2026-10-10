package registry

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"tank.local/tank/internal/encoding"
	"tank.local/tank/internal/integrity"
	"tank.local/tank/internal/metadata"
)

func workerManifest(t *testing.T, label string) metadata.Manifest {
	t.Helper()
	data := []byte(label)
	shards, err := encoding.Encode(data)
	if err != nil {
		t.Fatal(err)
	}
	root, _, err := integrity.BuildMerkleRoot(shards)
	if err != nil {
		t.Fatal(err)
	}
	records := make([]metadata.Shard, len(shards))
	for i, shard := range shards {
		records[i] = metadata.Shard{Index: i, Hash: integrity.Digest(shard).String()}
	}
	return metadata.Manifest{Version: 1, FileID: integrity.Digest(data).String(), Size: int64(len(data)), CreatedAt: time.Now().UTC(), Segments: []metadata.Segment{{Size: len(data), MerkleRoot: root.String(), Shards: records}}}
}
func workerStore(t *testing.T) *metadata.Store {
	t.Helper()
	store, err := metadata.Open(context.Background(), filepath.Join(t.TempDir(), "tank.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}
func workerConfig(url string) Config {
	return Config{RPCURL: url, Contract: "0x1111111111111111111111111111111111111111", Registrant: "0x2222222222222222222222222222222222222222"}
}

func TestWorkerRejectsUnsafeConfiguration(t *testing.T) {
	store := workerStore(t)
	for _, url := range []string{"https://example.com", "http://user:password@localhost", "http://localhost?token=x", "http://localhost#fragment", "file:///tmp/chain", "http://127.0.0.1.invalid", "http://192.168.1.1"} {
		if _, err := NewWorker(store, workerConfig(url)); err == nil {
			t.Fatalf("accepted unsafe RPC %s", url)
		}
	}
	for _, field := range []string{"contract", "registrant"} {
		cfg := workerConfig("http://localhost:8545")
		if field == "contract" {
			cfg.Contract = common.Address{}.Hex()
		} else {
			cfg.Registrant = common.Address{}.Hex()
		}
		if _, err := NewWorker(store, cfg); err == nil {
			t.Fatal("accepted zero " + field)
		}
	}
	if _, err := NewWorker(nil, workerConfig("http://localhost")); err == nil {
		t.Fatal("accepted missing store")
	}
	for _, url := range []string{"http://localhost:8545", "http://127.0.0.1:8545", "http://[::1]:8545"} {
		if _, err := NewWorker(store, workerConfig(url)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReconcilePaginatesAndPreservesCompletedJobs(t *testing.T) {
	ctx := context.Background()
	store := workerStore(t)
	worker, err := NewWorker(store, workerConfig("http://127.0.0.1:8545"))
	if err != nil {
		t.Fatal(err)
	}
	manifests := make([]metadata.Manifest, 101)
	for i := range manifests {
		manifests[i] = workerManifest(t, fmt.Sprintf("registration fixture %d", i))
		if err := store.Save(ctx, manifests[i]); err != nil {
			t.Fatal(err)
		}
	}
	if err := worker.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	job, err := store.ClaimRegistration(ctx, worker.Target(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteRegistration(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err := worker.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	for _, m := range manifests {
		status, err := store.GetRegistrationStatus(ctx, m.FileID, worker.Target())
		if err != nil {
			t.Fatal(err)
		}
		want := "pending"
		if m.FileID == job.FileID {
			want = "registered"
		}
		if status.State != want {
			t.Fatalf("state %s; want %s", status.State, want)
		}
	}
}

// Only this owned JSON-RPC fixture is contacted. No wallet, live chain, or user data.
type registryRPC struct {
	mu                       sync.Mutex
	chainID, code            string
	record                   []byte
	sends                    int
	unavailable, dropReceipt bool
}

func (fixture *registryRPC) handler(w http.ResponseWriter, r *http.Request) {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	var request struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		w.WriteHeader(400)
		return
	}
	response := map[string]any{"jsonrpc": "2.0", "id": request.ID}
	if fixture.unavailable {
		response["error"] = map[string]any{"code": -32000, "message": "synthetic RPC outage"}
	} else {
		switch request.Method {
		case "eth_chainId":
			response["result"] = fixture.chainID
		case "eth_getCode":
			response["result"] = fixture.code
		case "eth_call":
			if fixture.record == nil {
				response["error"] = map[string]any{"code": 3, "message": "execution reverted", "data": hexutil.Encode(crypto.Keccak256([]byte("RecordNotFound()"))[:4])}
			} else {
				response["result"] = hexutil.Encode(fixture.record)
			}
		case "eth_estimateGas":
			response["result"] = "0x10000"
		case "eth_sendTransaction":
			fixture.sends++
			response["result"] = common.HexToHash("0x1234").Hex()
		case "eth_getTransactionReceipt":
			if fixture.dropReceipt {
				response["error"] = map[string]any{"code": -32000, "message": "synthetic receipt outage"}
			} else {
				response["result"] = &types.Receipt{Status: types.ReceiptStatusSuccessful, TxHash: common.HexToHash("0x1234"), BlockHash: common.HexToHash("0x5678"), BlockNumber: big.NewInt(1), Logs: []*types.Log{}}
			}
		default:
			response["error"] = map[string]any{"code": -32601, "message": "unexpected fixture method"}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
func rpcRecord(t *testing.T, m metadata.Manifest) []byte {
	t.Helper()
	commitment, err := m.Commitment()
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 96)
	copy(data, commitment[:])
	binary.BigEndian.PutUint64(data[56:64], uint64(m.Size))
	binary.BigEndian.PutUint64(data[88:96], 123)
	return data
}
func TestOpenRejectsWrongChainAndMissingContract(t *testing.T) {
	for _, cfg := range []struct{ name, chainID, code string }{{"wrong chain", "0x1", "0x01"}, {"missing contract", "0x7a69", "0x"}} {
		t.Run(cfg.name, func(t *testing.T) {
			fixture := &registryRPC{chainID: cfg.chainID, code: cfg.code}
			server := httptest.NewServer(http.HandlerFunc(fixture.handler))
			defer server.Close()
			if client, err := Open(context.Background(), workerConfig(server.URL)); err == nil {
				client.Close()
				t.Fatal("accepted incompatible chain")
			}
		})
	}
}
func TestWorkerExistingRecordAndOutage(t *testing.T) {
	for _, mode := range []string{"matching", "conflicting", "outage"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			store := workerStore(t)
			m := workerManifest(t, "record fixture")
			if err := store.Save(ctx, m); err != nil {
				t.Fatal(err)
			}
			fixture := &registryRPC{chainID: "0x7a69", code: "0x01", record: rpcRecord(t, m)}
			if mode == "conflicting" {
				fixture.record[0] ^= 1
			}
			if mode == "outage" {
				fixture.unavailable = true
			}
			server := httptest.NewServer(http.HandlerFunc(fixture.handler))
			defer server.Close()
			worker, err := NewWorker(store, workerConfig(server.URL))
			if err != nil {
				t.Fatal(err)
			}
			if err := worker.Reconcile(ctx); err != nil {
				t.Fatal(err)
			}
			worked, err := worker.Process(ctx)
			if err != nil || !worked {
				t.Fatalf("process: %v %v", worked, err)
			}
			status, err := store.GetRegistrationStatus(ctx, m.FileID, worker.Target())
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]string{"matching": "registered", "conflicting": "failed", "outage": "pending"}[mode]
			if status.State != want || status.Attempts != 1 {
				t.Fatalf("status %+v; want %s", status, want)
			}
			fixture.mu.Lock()
			sends := fixture.sends
			fixture.mu.Unlock()
			if sends != 0 {
				t.Fatal("existing record/outage sent transaction")
			}
			if mode == "outage" {
				if !strings.Contains(status.LastError, "synthetic RPC outage") {
					t.Fatal("retry reason missing")
				}
				if _, err := store.Load(ctx, m.FileID); err != nil {
					t.Fatal("outage affected storage")
				}
			}
		})
	}
}
func TestWorkerResumesSavedTransactionWithoutResubmitting(t *testing.T) {
	ctx := context.Background()
	store := workerStore(t)
	m := workerManifest(t, "resume fixture")
	if err := store.Save(ctx, m); err != nil {
		t.Fatal(err)
	}
	fixture := &registryRPC{chainID: "0x7a69", code: "0x01", dropReceipt: true}
	server := httptest.NewServer(http.HandlerFunc(fixture.handler))
	defer server.Close()
	worker, err := NewWorker(store, workerConfig(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	job, err := store.ClaimRegistration(ctx, worker.Target(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	permanent, err := worker.register(ctx, job)
	if err == nil || permanent {
		t.Fatalf("receipt outage: permanent=%v error=%v", permanent, err)
	}
	status, err := store.GetRegistrationStatus(ctx, m.FileID, worker.Target())
	if err != nil {
		t.Fatal(err)
	}
	if status.TransactionHash != common.HexToHash("0x1234").Hex() {
		t.Fatal("transaction not durably saved")
	}
	if err := store.RetryRegistration(ctx, job, time.Second, "synthetic receipt outage"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond) // Wait for the persisted retry deadline.
	job, err = store.ClaimRegistration(ctx, worker.Target(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	fixture.mu.Lock()
	fixture.dropReceipt = false
	fixture.mu.Unlock()
	// Receipt succeeds, but readback still missing: retain transaction for another retry.
	permanent, err = worker.register(ctx, job)
	if err == nil || permanent || !strings.Contains(err.Error(), "missing after transaction") {
		t.Fatalf("readback gap: %v %v", permanent, err)
	}
	fixture.mu.Lock()
	fixture.record = rpcRecord(t, m)
	sends := fixture.sends
	fixture.mu.Unlock()
	if sends != 1 {
		t.Fatal("saved transaction was resubmitted")
	}
	if err := store.RetryRegistration(ctx, job, time.Second, "synthetic readback gap"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond) // Wait for the persisted retry deadline.
	worked, err := worker.Process(ctx)
	if err != nil || !worked {
		t.Fatalf("recovery: %v %v", worked, err)
	}
	status, err = store.GetRegistrationStatus(ctx, m.FileID, worker.Target())
	if err != nil || status.State != "registered" || status.Attempts != 3 {
		t.Fatalf("recovery state %+v %v", status, err)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if fixture.sends != 1 {
		t.Fatal("recovery submitted duplicate transaction")
	}
}

func TestOpenAcceptsCaseInsensitiveLocalhost(t *testing.T) {
	fixture := &registryRPC{chainID: "0x7a69", code: "0x01"}
	server := httptest.NewServer(http.HandlerFunc(fixture.handler))
	defer server.Close()
	cfg := workerConfig(strings.Replace(server.URL, "127.0.0.1", "LOCALHOST", 1))
	if _, err := NewWorker(workerStore(t), cfg); err != nil {
		t.Fatal(err)
	}
	client, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("worker accepted loopback configuration but client rejected it: %v", err)
	}
	client.Close()
}
