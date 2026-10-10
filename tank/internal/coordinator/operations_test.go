package coordinator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"tank.local/tank/internal/limits"
	"tank.local/tank/internal/node"
	"testing"
	"time"
)

func TestOperationsRequiresAdministratorAndPreservesLiveness(t *testing.T) {
	fixture := newResourceFixture(t, limits.Default())
	for _, test := range []struct {
		token  string
		status int
	}{{"", 401}, {fixture.aliceToken, 403}, {strings.Repeat("a", 32), 200}} {
		request := httptest.NewRequest(http.MethodGet, "/ops/status", nil)
		if test.token != "" {
			request.Header.Set("Authorization", "Bearer "+test.token)
		}
		response := httptest.NewRecorder()
		fixture.handler.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Fatalf("status %d; want %d", response.Code, test.status)
		}
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("operational response cached")
		}
		if test.status == 200 {
			var result struct {
				Ready    bool         `json:"upload_ready"`
				Degraded bool         `json:"degraded"`
				Nodes    []nodeStatus `json:"nodes"`
				Audit    AuditStatus  `json:"last_audit"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if !result.Ready || result.Degraded || len(result.Nodes) != 3 || result.Audit.State != "never" {
				t.Fatal("healthy cluster status incorrect")
			}
			for _, value := range []string{"token", "filename", "file_id", "127.0.0.1", fixture.aliceToken} {
				if strings.Contains(response.Body.String(), value) {
					t.Fatal("operational response exposes unnecessary private detail")
				}
			}
		}
	}
	if err := fixture.backends[0].Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/ops/status", nil)
	request.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 32))
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request)
	var degraded struct {
		Ready    bool `json:"upload_ready"`
		Degraded bool `json:"degraded"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &degraded); err != nil {
		t.Fatal(err)
	}
	if degraded.Ready || !degraded.Degraded {
		t.Fatal("closed storage did not affect readiness")
	}
	fixture.store.Close()
	response = httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request)
	if response.Code != 503 {
		t.Fatal("closed database appeared operational")
	}
	response = httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))
	if response.Code != 200 {
		t.Fatal("liveness depends on operational dependencies")
	}
}
func TestOperationsReportsQuotaJobsAndAuditWithoutInternalErrors(t *testing.T) {
	bounds := limits.Default()
	bounds.TotalStorageBytes = 1
	fixture := newResourceFixture(t, bounds)
	m := authTestManifest(t)
	ctx := context.Background()
	if err := fixture.store.Save(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.EnqueueRepair(ctx, m.FileID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.EnqueueRegistration(ctx, m.FileID, "31337:test:test"); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.AuditOnce(ctx); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/ops/status", nil)
	request.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 32))
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request)
	var result struct {
		Ready    bool                                           `json:"upload_ready"`
		Metadata struct{ Files, Bytes, Repairs, Pending int64 } `json:"metadata"`
		Audit    AuditStatus                                    `json:"last_audit"`
	}
	// Inspect aggregates explicitly; no ID or filename fields are part of this API.
	var body map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body["upload_ready"], &result.Ready); err != nil {
		t.Fatal(err)
	}
	if result.Ready {
		t.Fatal("exhausted total quota reported ready")
	}
	var aggregate map[string]int64
	if err := json.Unmarshal(body["metadata"], &aggregate); err != nil {
		t.Fatal(err)
	}
	if aggregate["files"] != 1 || aggregate["committed_bytes"] != m.Size || aggregate["repair_jobs"] != 1 || aggregate["registration_pending"] != 1 {
		t.Fatal("operations lost accounting/jobs")
	}
	if err := json.Unmarshal(body["last_audit"], &result.Audit); err != nil {
		t.Fatal(err)
	}
	if result.Audit.State != "completed" || result.Audit.CompletedAt.IsZero() {
		t.Fatal("completed audit not recorded")
	}
	if strings.Contains(response.Body.String(), m.FileID) {
		t.Fatal("operations leaked a file identifier")
	}
}
func TestOperationsBoundsStalledNodeChecks(t *testing.T) {
	fixture := newResourceFixture(t, limits.Default())
	stalled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer stalled.Close()
	client, err := node.NewClient(stalled.URL, strings.Repeat("n", 32))
	if err != nil {
		t.Fatal(err)
	}
	delete(fixture.service.byURL, fixture.service.nodes[0].BaseURL)
	fixture.service.nodes[0] = client
	fixture.service.byURL[client.BaseURL] = client
	request := httptest.NewRequest(http.MethodGet, "/ops/status", nil)
	request.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 32))
	response := httptest.NewRecorder()
	started := time.Now()
	fixture.handler.ServeHTTP(response, request)
	if time.Since(started) > 3*time.Second {
		t.Fatal("stalled operational check exceeded bound")
	}
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"upload_ready":false`) {
		t.Fatal("stalled node status wrong")
	}
}
