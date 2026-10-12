package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"tank.local/tank/internal/metadata"
)

type ambiguousSubmitter struct{ raw [][]byte }

func (s *ambiguousSubmitter) Submit(_ context.Context, transaction *types.Transaction, p TestnetPolicy) error {
	if !p.allows(transaction) {
		return ErrSubmission
	}
	raw, err := transaction.MarshalBinary()
	if err != nil {
		return err
	}
	s.raw = append(s.raw, raw)
	return errors.New("synthetic-provider-secret-after-acceptance")
}

func TestAuthorizedWorkerAmbiguousSubmissionSurvivesDatabaseRestart(t *testing.T) {
	ctx := preparationContext(t)
	path := filepath.Join(t.TempDir(), "restart.sqlite")
	store, err := metadata.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	signer := signerFixture(t)
	p := policyFixture(signer)
	p.Confirmations = 1
	reader := &preparationFixture{}
	submitter := &ambiguousSubmitter{}
	w, err := NewBroadcastSignedWorker(store, p, signer, reader, submitter, big.NewInt(4000000000000000))
	if err != nil {
		t.Fatal(err)
	}
	w.retryDelay = time.Second
	m := workerManifest(t, "authorized immutable restart")
	if err := store.Save(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := w.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if worked, err := w.Process(ctx); err != nil || !worked {
		t.Fatalf("first submission: %v", err)
	}
	status, err := store.GetRegistrationStatus(ctx, m.FileID, w.Target())
	if err != nil || status.State == "registered" || strings.Contains(status.LastError, "synthetic-provider-secret") {
		t.Fatal("ambiguous submission falsely confirmed or leaked secret")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = metadata.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	w, err = NewBroadcastSignedWorker(store, p, signer, reader, submitter, big.NewInt(4000000000000000))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	if worked, err := w.Process(ctx); err != nil || !worked {
		t.Fatalf("restart submission: %v", err)
	}
	if len(submitter.raw) != 2 || !bytes.Equal(submitter.raw[0], submitter.raw[1]) || signer.calls.Load() != 1 || reader.calls != 1 {
		t.Fatal("restart did not reuse journaled bytes, nonce and signature")
	}
}

func TestAuthorizedWorkerAggregateBudgetBlocksSubmission(t *testing.T) {
	ctx := preparationContext(t)
	store := workerStore(t)
	signer := signerFixture(t)
	p := policyFixture(signer)
	p.Confirmations = 1
	submitter := &ambiguousSubmitter{}
	w, err := NewBroadcastSignedWorker(store, p, signer, &preparationFixture{}, submitter, big.NewInt(1))
	if err != nil {
		t.Fatal(err)
	}
	m := workerManifest(t, "aggregate budget fails closed")
	if err := store.Save(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := w.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if worked, err := w.Process(ctx); err != nil || !worked {
		t.Fatalf("budget processing failed: %v", err)
	}
	if len(submitter.raw) != 0 {
		t.Fatal("transaction over aggregate budget reached submission")
	}
	status, err := store.GetRegistrationStatus(ctx, m.FileID, w.Target())
	if err != nil || status.State == "registered" || status.TransactionHash == "" {
		t.Fatal("bounded intent lost or falsely confirmed")
	}
}

func TestAuthorizedSubmissionRejectsMainnetBeforeSendAndRedactsErrors(t *testing.T) {
	for _, mode := range []string{"valid", "rpc_mainnet", "rpc_ethereum", "configured_mainnet", "transaction_mainnet", "ambiguous"} {
		t.Run(mode, func(t *testing.T) {
			fixture := fixtureReader(t)
			sends := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				encoded := new(bytes.Buffer)
				encoded.ReadFrom(r.Body)
				var request struct {
					ID     json.RawMessage `json:"id"`
					Method string          `json:"method"`
				}
				json.Unmarshal(encoded.Bytes(), &request)
				if request.Method == "eth_sendRawTransaction" {
					sends++
					result := map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": "0x" + strings.Repeat("1", 64)}
					if mode == "ambiguous" {
						result = map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": -32000, "message": "synthetic-provider-secret-after-send"}}
					}
					json.NewEncoder(w).Encode(result)
					return
				}
				r.Body = http.NoBody
				r.Body = newReadCloser(encoded.Bytes())
				fixture.handler(w, r)
			}))
			defer server.Close()
			signer := signerFixture(t)
			p := policyFixture(signer)
			cfg := arcReaderConfig(server.URL)
			cfg.Registrant = signer.Address().Hex()
			reader, err := openTestnetReader(context.Background(), cfg, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			chain := ArcTestnetChainID
			switch mode {
			case "rpc_mainnet":
				fixture.chainID = fmt.Sprintf("0x%x", 5042)
			case "rpc_ethereum":
				fixture.chainID = "0x1"
			case "configured_mainnet":
				p.ChainID = 5042
			case "transaction_mainnet":
				chain = 5042
			}
			tx := types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(chain), Nonce: 0, Gas: 50000, GasFeeCap: big.NewInt(ArcMinimumFee), GasTipCap: big.NewInt(0), To: &p.Contract, Value: big.NewInt(0), Data: []byte{1}})
			signed, err := types.SignTx(tx, types.LatestSignerForChainID(big.NewInt(chain)), signer.key)
			if err != nil {
				t.Fatal(err)
			}
			err = reader.Submit(context.Background(), signed, p)
			if mode == "valid" {
				if err != nil || sends != 1 {
					t.Fatal("valid signed submission rejected")
				}
				return
			}
			if !errors.Is(err, ErrSubmission) || strings.Contains(err.Error(), "synthetic-provider-secret") {
				t.Fatal("unsafe submission accepted or provider error exposed")
			}
			if mode == "ambiguous" {
				if sends != 1 {
					t.Fatal("ambiguous send missing")
				}
			} else if sends != 0 {
				t.Fatal("mainnet reached submission")
			}
		})
	}
}

type byteReadCloser struct{ *bytes.Reader }

func (byteReadCloser) Close() error             { return nil }
func newReadCloser(value []byte) byteReadCloser { return byteReadCloser{bytes.NewReader(value)} }
