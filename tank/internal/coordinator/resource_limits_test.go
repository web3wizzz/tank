package coordinator

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tank.local/tank/internal/integrity"
	"tank.local/tank/internal/limits"
	"tank.local/tank/internal/metadata"
	"tank.local/tank/internal/node"
	"tank.local/tank/internal/storage"
)

type resourceFixture struct {
	store                *metadata.Store
	service              *Service
	handler              http.Handler
	backends             []*storage.Filesystem
	alice, bob           metadata.AccessKey
	aliceToken, bobToken string
}

func newResourceFixture(t *testing.T, bounds limits.Config) *resourceFixture {
	t.Helper()
	ctx := context.Background()
	store, err := metadata.Open(ctx, filepath.Join(t.TempDir(), "tank.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	fixture := &resourceFixture{store: store}
	var clients []*node.Client
	token := strings.Repeat("n", 32)
	for index := 0; index < 3; index++ {
		backend, err := storage.NewFilesystemWithQuota(t.TempDir(), bounds.NodeStorageBytes, bounds.NodeFileLimit)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { backend.Close() })
		handler, err := node.NewHandler(backend, token)
		if err != nil {
			t.Fatal(err)
		}
		server := httptest.NewServer(handler)
		t.Cleanup(server.Close)
		client, err := node.NewClient(server.URL, token)
		if err != nil {
			t.Fatal(err)
		}
		clients = append(clients, client)
		fixture.backends = append(fixture.backends, backend)
	}
	fixture.service, err = NewServiceWithLimits(store, clients, 4<<20, bounds)
	if err != nil {
		t.Fatal(err)
	}
	fixture.handler, err = NewHandler(fixture.service, strings.Repeat("a", 32))
	if err != nil {
		t.Fatal(err)
	}
	fixture.alice, fixture.aliceToken, err = store.CreateAccessKey(ctx, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	fixture.bob, fixture.bobToken, err = store.CreateAccessKey(ctx, "Bob")
	if err != nil {
		t.Fatal(err)
	}
	return fixture
}
func (f *resourceFixture) request(method, path, token string, data []byte) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewReader(data))
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	f.handler.ServeHTTP(response, request)
	return response
}
func TestUploadLimitsQuotasDeduplicationAndUserIsolation(t *testing.T) {
	bounds := limits.Default()
	bounds.MaxFileBytes = 8
	bounds.UserStorageBytes = 5
	bounds.TotalStorageBytes = 7
	bounds.RequestsPerMinute = 0
	fixture := newResourceFixture(t, bounds)
	for _, step := range []struct {
		token  string
		data   string
		status int
	}{
		{fixture.aliceToken, "abc", 200}, {fixture.aliceToken, "abc", 200},
		{fixture.aliceToken, "def", 507}, {fixture.bobToken, "abc", 200}, {fixture.bobToken, "de", 200},
		{strings.Repeat("a", 32), "ghi", 507}, {fixture.aliceToken, "123456789", 413},
	} {
		response := fixture.request("POST", "/tank", step.token, []byte(step.data))
		if response.Code != step.status {
			t.Fatalf("request status %d, want %d", response.Code, step.status)
		}
	}
	request := httptest.NewRequest("POST", "/tank", bytes.NewReader([]byte("123456789")))
	request.ContentLength = -1
	request.Header.Set("Authorization", "Bearer "+fixture.aliceToken)
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request)
	if response.Code != 413 {
		t.Fatal("streamed oversized upload bypassed body cap")
	}
	for _, step := range []struct {
		id    string
		bytes int64
	}{{fixture.alice.PrincipalID, 3}, {fixture.bob.PrincipalID, 5}} {
		usage, err := fixture.store.StorageUsage(context.Background(), step.id)
		if err != nil || usage.UserBytes != step.bytes || usage.TotalBytes != 5 {
			t.Fatal("incorrect committed quota accounting")
		}
	}
	id := integrity.Digest([]byte("def")).String()
	if _, err := fixture.store.Load(context.Background(), id); !errors.Is(err, metadata.ErrNotFound) {
		t.Fatal("rejected upload published a manifest")
	}
	if _, err := fixture.backends[0].Get(context.Background(), storage.ShardKey{FileID: id}); !errors.Is(err, storage.ErrShardNotFound) {
		t.Fatal("quota rejection wrote shards before admission")
	}
	if response := fixture.request("GET", "/retrieve/"+integrity.Digest([]byte("abc")).String(), fixture.aliceToken, nil); response.Code != 200 || !bytes.Equal(response.Body.Bytes(), []byte("abc")) {
		t.Fatal("limits changed an existing download")
	}
}
func TestRateBudgetSurvivesCredentialRenewal(t *testing.T) {
	bounds := limits.Default()
	bounds.RequestsPerMinute = 1
	bounds.Burst = 1
	fixture := newResourceFixture(t, bounds)
	if response := fixture.request("GET", "/list", fixture.aliceToken, nil); response.Code != 200 {
		t.Fatal("first request refused")
	}
	_, replacement, err := fixture.store.IssueAccessKey(context.Background(), fixture.alice.PrincipalID)
	if err != nil {
		t.Fatal(err)
	}
	response := fixture.request("GET", "/list", replacement, nil)
	if response.Code != 429 || response.Header().Get("Retry-After") == "" {
		t.Fatal("replacement credential reset principal rate limit")
	}
	if response := fixture.request("GET", "/list", fixture.bobToken, nil); response.Code != 200 {
		t.Fatal("Alice's rate budget affected Bob")
	}
}
func TestGlobalAdmissionProtectsAuthenticationAndLeavesHealthAvailable(t *testing.T) {
	bounds := limits.Default()
	bounds.MaxConcurrent = 2
	bounds.MaxPerUser = 1
	bounds.RequestsPerMinute = 0
	fixture := newResourceFixture(t, bounds)
	first, ok := fixture.service.governor.Begin()
	if !ok {
		t.Fatal("first slot unavailable")
	}
	second, ok := fixture.service.governor.Begin()
	if !ok {
		t.Fatal("second slot unavailable")
	}
	if response := fixture.request("POST", "/tank", "invalid", []byte("abc")); response.Code != 429 || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("unknown credentials bypassed global admission or lost no-store policy")
	}
	if response := fixture.request("GET", "/health", "", nil); response.Code != 200 {
		t.Fatal("saturation blocked health monitoring")
	}
	first()
	second()
	if response := fixture.request("POST", "/tank", "invalid", []byte("abc")); response.Code != 401 {
		t.Fatal("global release did not restore authentication")
	}
}
func TestUploadLeaseBusyAndNodeCapacityReturnRetryableErrors(t *testing.T) {
	bounds := limits.Default()
	bounds.RequestsPerMinute = 0
	bounds.NodeStorageBytes = 1
	fixture := newResourceFixture(t, bounds)
	response := fixture.request("POST", "/tank", fixture.aliceToken, []byte("abc"))
	if response.Code != 507 {
		t.Fatal("node capacity error was not preserved")
	}
	usage, err := fixture.store.StorageUsage(context.Background(), fixture.alice.PrincipalID)
	if err != nil || usage.UserBytes != 0 || usage.TotalBytes != 0 {
		t.Fatal("failed node storage published quota usage")
	}
	ctx, cancel := context.WithTimeout(context.Background(), limits.Default().RequestTimeout)
	defer cancel()
	token, err := fixture.store.AcquireUploadLease(ctx)
	if err != nil {
		t.Fatal("failed upload leaked its admission lease")
	}
	defer fixture.store.ReleaseUploadLease(context.Background(), token)
	response = fixture.request("POST", "/tank", fixture.aliceToken, []byte("x"))
	if response.Code != 429 || response.Header().Get("Retry-After") == "" {
		t.Fatal("busy upload lease was not retryable")
	}
}

func TestConfiguredDeadlineInterruptsAnAuthenticatedSlowBody(t *testing.T) {
	bounds := limits.Default()
	bounds.RequestTimeout = 200 * time.Millisecond
	bounds.RequestsPerMinute = 0
	fixture := newResourceFixture(t, bounds)
	server := httptest.NewServer(fixture.handler)
	defer server.Close()
	address := strings.TrimPrefix(server.URL, "http://")
	connection, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(connection, "POST /tank HTTP/1.1\r\nHost: %s\r\nAuthorization: Bearer %s\r\nContent-Length: 100\r\n\r\n", address, fixture.aliceToken)
	if err != nil {
		t.Fatal("failed to send test request headers")
	}
	response, err := http.ReadResponse(bufio.NewReader(connection), nil)
	if err != nil {
		t.Fatal("configured timeout did not return a bounded response")
	}
	response.Body.Close()
	if response.StatusCode != 408 && response.StatusCode != 504 {
		t.Fatal("slow body was not rejected")
	}
	usage, err := fixture.store.StorageUsage(context.Background(), fixture.alice.PrincipalID)
	if err != nil || usage.UserBytes != 0 || usage.TotalBytes != 0 {
		t.Fatal("timed-out upload changed quota usage")
	}
}
