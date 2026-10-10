package node

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"tank.local/tank/internal/integrity"
	"tank.local/tank/internal/storage"
	"testing"
	"time"
)

func TestNodeStatusAuthenticationCapacityAndClosedBackend(t *testing.T) {
	backend, err := storage.NewFilesystemWithQuota(t.TempDir(), 4, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	token := strings.Repeat("n", 32)
	handler, err := NewHandler(backend, token)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/ops/status", nil))
	if response.Code != 401 {
		t.Fatal("capacity exposed without authentication")
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	client, err := NewClient(server.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	bytes := []byte("test")
	key := storage.ShardKey{FileID: integrity.Digest(bytes).String(), Index: 0, Segment: 0}
	if err := backend.Put(context.Background(), key, bytes, integrity.Digest(bytes)); err != nil {
		t.Fatal(err)
	}
	status, err := client.Status(context.Background())
	if err != nil || status.UsedBytes != 4 || status.UsedFiles != 1 || !status.AtCapacity {
		t.Fatal("capacity accounting missing")
	}
	backend.Close()
	if _, err := client.Status(context.Background()); err == nil {
		t.Fatal("closed backend reported available")
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/health", nil))
	if response.Code != 200 {
		t.Fatal("liveness changed with closed backend")
	}
}
func TestNodeStatusRejectsMalformedReportsAndTimesOut(t *testing.T) {
	for _, body := range []string{`{}`, `null`, `{"used_bytes":-1,"byte_limit":0,"used_files":0,"file_limit":0}`, strings.Repeat("x", 4097), `{"used_bytes":0,"byte_limit":0,"used_files":0,"file_limit":0} trailing`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
		client, err := NewClient(server.URL, strings.Repeat("n", 32))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.Status(context.Background()); err == nil {
			t.Fatal("invalid capacity accepted")
		}
		server.Close()
	}
	stalled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer stalled.Close()
	client, err := NewClient(stalled.URL, strings.Repeat("n", 32))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := client.Status(ctx); err == nil {
		t.Fatal("stalled status succeeded")
	}
	if time.Since(started) > time.Second {
		t.Fatal("status ignored deadline")
	}
}
