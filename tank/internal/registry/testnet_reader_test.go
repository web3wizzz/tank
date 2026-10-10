package registry

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"tank.local/tank/internal/integrity"
	"testing"
	"time"
)

type readRPCFixture struct {
	mu                                   sync.Mutex
	chainID, code                        string
	header                               *types.Header
	receipt                              *types.Receipt
	record                               []byte
	methods                              []string
	pin                                  bool
	reorg, providerError, missingReceipt bool
	headers                              int
}

func fixtureReader(t *testing.T) *readRPCFixture {
	t.Helper()
	header := &types.Header{Number: big.NewInt(10), Difficulty: big.NewInt(0), Time: 123, Extra: []byte("canonical fixture")}
	record := make([]byte, 96)
	record[0] = 1
	binary.BigEndian.PutUint64(record[56:64], 10)
	binary.BigEndian.PutUint64(record[88:96], 123)
	return &readRPCFixture{chainID: "0x14a34", code: "0x01", header: header, receipt: &types.Receipt{TxHash: common.HexToHash("0x1234"), Status: 1, BlockHash: header.Hash(), BlockNumber: big.NewInt(10), Logs: []*types.Log{}}, record: record}
}
func (f *readRPCFixture) handler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var request struct {
		ID     json.RawMessage   `json:"id"`
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		w.WriteHeader(400)
		return
	}
	f.methods = append(f.methods, request.Method)
	result := map[string]any{"jsonrpc": "2.0", "id": request.ID}
	if f.providerError {
		result["error"] = map[string]any{"code": -32000, "message": "synthetic-provider-secret-do-not-log"}
	} else {
		switch request.Method {
		case "eth_chainId":
			result["result"] = f.chainID
		case "eth_getCode":
			result["result"] = f.code
		case "eth_getTransactionCount":
			result["result"] = "0x7"
		case "eth_getTransactionReceipt":
			if f.missingReceipt {
				result["result"] = nil
			} else {
				result["result"] = f.receipt
			}
		case "eth_getBlockByNumber":
			f.headers++
			header := *f.header
			if f.reorg && f.headers > 1 {
				header.Extra = []byte("reorg fixture")
			}
			result["result"] = &header
		case "eth_call":
			if len(request.Params) == 2 {
				var pin struct {
					Hash      string `json:"blockHash"`
					Canonical bool   `json:"requireCanonical"`
				}
				json.Unmarshal(request.Params[1], &pin)
				f.pin = pin.Hash == f.receipt.BlockHash.Hex() && pin.Canonical
			}
			result["result"] = hexutil.Encode(f.record)
		case "eth_blockNumber":
			result["result"] = "0x15"
		default:
			result["error"] = map[string]any{"code": -32601, "message": "forbidden fixture method"}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
func TestTestnetReaderUsesOnlyChainBoundCanonicalReads(t *testing.T) {
	fixture := fixtureReader(t)
	server := httptest.NewTLSServer(http.HandlerFunc(fixture.handler))
	defer server.Close()
	cfg := workerConfig(server.URL + "/synthetic-private-provider-path")
	reader, err := openTestnetReader(context.Background(), cfg, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	nonce, err := reader.PendingNonce(context.Background())
	if err != nil || nonce != 7 {
		t.Fatal("pending nonce read failed")
	}
	observation, err := reader.Observation(context.Background(), fixture.receipt.TxHash, integrity.Digest([]byte("read fixture")))
	if err != nil {
		t.Fatal(err)
	}
	if observation.Head != 21 || observation.ReadbackBlockHash != fixture.receipt.BlockHash || observation.CanonicalHeader.Hash() != fixture.receipt.BlockHash || observation.Record.FileSize != 10 {
		t.Fatal("canonical observation incorrect")
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if !fixture.pin || fixture.headers != 2 {
		t.Fatal("readback not pinned or canonical header not rechecked")
	}
	for _, method := range fixture.methods {
		if strings.Contains(method, "send") || strings.Contains(method, "sign") {
			t.Fatal("read-only reader invoked submission or signing")
		}
	}
}
func TestTestnetReaderRejectsWrongChainMissingCodeAndUntrustedTLS(t *testing.T) {
	for _, mode := range []string{"wrong_chain", "missing_code", "untrusted_tls", "plaintext", "user_info"} {
		t.Run(mode, func(t *testing.T) {
			fixture := fixtureReader(t)
			if mode == "wrong_chain" {
				fixture.chainID = "0x7a69"
			}
			if mode == "missing_code" {
				fixture.code = "0x"
			}
			server := httptest.NewTLSServer(http.HandlerFunc(fixture.handler))
			defer server.Close()
			cfg := workerConfig(server.URL)
			var err error
			switch mode {
			case "untrusted_tls":
				_, err = OpenTestnetReader(context.Background(), cfg)
			case "plaintext":
				cfg.RPCURL = strings.Replace(cfg.RPCURL, "https:", "http:", 1)
				_, err = openTestnetReader(context.Background(), cfg, server.Client())
			case "user_info":
				cfg.RPCURL = strings.Replace(cfg.RPCURL, "https://", "https://user:synthetic-secret@", 1)
				_, err = openTestnetReader(context.Background(), cfg, server.Client())
			default:
				_, err = openTestnetReader(context.Background(), cfg, server.Client())
			}
			if err == nil || strings.Contains(err.Error(), "synthetic-secret") {
				t.Fatal("invalid reader accepted or configuration secret exposed")
			}
		})
	}
}
func TestTestnetReaderDetectsReorgAndRedactsProviderErrors(t *testing.T) {
	for _, mode := range []string{"reorg", "provider_error", "missing_receipt", "network_changed"} {
		t.Run(mode, func(t *testing.T) {
			fixture := fixtureReader(t)
			server := httptest.NewTLSServer(http.HandlerFunc(fixture.handler))
			defer server.Close()
			reader, err := openTestnetReader(context.Background(), workerConfig(server.URL), server.Client())
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			fixture.mu.Lock()
			switch mode {
			case "reorg":
				fixture.reorg = true
			case "provider_error":
				fixture.providerError = true
			case "missing_receipt":
				fixture.missingReceipt = true
			case "network_changed":
				fixture.chainID = "0x2105"
			}
			fixture.mu.Unlock()
			if _, err := reader.Observation(context.Background(), fixture.receipt.TxHash, integrity.Digest([]byte("read fixture"))); err == nil || strings.Contains(err.Error(), "synthetic-provider-secret") {
				t.Fatal("invalid observation accepted or RPC secret exposed")
			}
		})
	}
}
func TestTestnetReaderRefusesRedirectsAndHonorsCallerDeadline(t *testing.T) {
	reached := false
	destination := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	defer destination.Close()
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 302) }))
	defer redirect.Close()
	if reader, err := openTestnetReader(context.Background(), workerConfig(redirect.URL), redirect.Client()); err == nil {
		reader.Close()
		t.Fatal("provider redirect accepted")
	}
	if reached {
		t.Fatal("unselected redirect destination contacted")
	}
	stalled := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	}))
	defer stalled.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	if reader, err := openTestnetReader(ctx, workerConfig(stalled.URL), stalled.Client()); err == nil {
		reader.Close()
		t.Fatal("stalled reader succeeded")
	}
	if time.Since(started) > time.Second {
		t.Fatal("reader ignored caller deadline")
	}
}

func TestTestnetReaderBoundsProviderResponse(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := `{"jsonrpc":"2.0","id":1,"result":"0x14a34"}` + strings.Repeat(" ", 1<<20)
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.Write([]byte(body))
	}))
	defer server.Close()
	if reader, err := openTestnetReader(context.Background(), workerConfig(server.URL), server.Client()); err == nil {
		reader.Close()
		t.Fatal("oversized provider response accepted")
	}
}
