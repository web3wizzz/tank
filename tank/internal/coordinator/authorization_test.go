package coordinator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tank.local/tank/internal/encoding"
	"tank.local/tank/internal/integrity"
	"tank.local/tank/internal/metadata"
	"tank.local/tank/internal/node"
)

func authTestManifest(t *testing.T) metadata.Manifest {
	t.Helper()
	data := []byte("Private authorization test document")
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
		records[i] = metadata.Shard{
			Index: i, Hash: integrity.Digest(shard).String(),
		}
	}
	return metadata.Manifest{
		Version: 1, FileID: integrity.Digest(data).String(),
		Size: int64(len(data)), CreatedAt: time.Now().UTC(),
		Segments: []metadata.Segment{{
			Index: 0, Size: len(data),
			MerkleRoot: root.String(), Shards: records,
		}},
	}
}

func TestHTTPUserIsolation(t *testing.T) {
	ctx := context.Background()
	store, err := metadata.Open(ctx, filepath.Join(t.TempDir(), "tank.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	aliceKey, aliceToken, err := store.CreateAccessKey(ctx, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	_, bobToken, err := store.CreateAccessKey(ctx, "Bob")
	if err != nil {
		t.Fatal(err)
	}

	m := authTestManifest(t)
	if err := store.Save(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveFilename(ctx, m.FileID, "admin-name.pdf"); err != nil {
		t.Fatal(err)
	}
	if err := store.GrantFileAccess(ctx, aliceKey.PrincipalID, m.FileID, "alice-name.pdf"); err != nil {
		t.Fatal(err)
	}

	var clients []*node.Client
	for _, address := range []string{
		"http://127.0.0.1:9101",
		"http://127.0.0.1:9102",
		"http://127.0.0.1:9103",
	} {
		client, err := node.NewClient(address, strings.Repeat("n", 64))
		if err != nil {
			t.Fatal(err)
		}
		clients = append(clients, client)
	}
	service, err := NewService(store, clients, encoding.MaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	admin := strings.Repeat("a", 64)
	base, err := NewHandler(service, admin)
	if err != nil {
		t.Fatal(err)
	}
	target := "31337:0x5fbdb2315678afecb367f032d93f642f64180aa3:0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266"
	handler := newRegistrationHandler(base, service, store, admin, target)

	request := func(method, path, token string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, nil)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}

	cases := []struct {
		name, method, path, token string
		status                    int
	}{
		{"anonymous list", "GET", "/list", "", 401},
		{"invalid credential", "GET", "/list", "invalid", 401},
		{"Alice metadata", "GET", "/files/" + m.FileID + "/info", aliceToken, 200},
		{"Bob metadata", "GET", "/files/" + m.FileID + "/info", bobToken, 404},
		{"Bob retrieval", "GET", "/retrieve/" + m.FileID, bobToken, 404},
		{"Alice registration", "GET", "/registrations/" + m.FileID, aliceToken, 200},
		{"Bob registration", "GET", "/registrations/" + m.FileID, bobToken, 404},
		{"Alice repair", "POST", "/repair/" + m.FileID, aliceToken, 403},
		{"Bob repair", "POST", "/repair/" + m.FileID, bobToken, 403},
		{"admin metadata", "GET", "/files/" + m.FileID + "/info", admin, 200},
		{"public health", "GET", "/health", "", 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := request(tc.method, tc.path, tc.token)
			if w.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.status, w.Body.String())
			}
		})
	}

	for _, tc := range []struct {
		token string
		count int
	}{{aliceToken, 1}, {bobToken, 0}, {admin, 1}} {
		w := request(http.MethodGet, "/list", tc.token)
		var ids []string
		if w.Code != 200 {
			t.Fatalf("list failed: %d", w.Code)
		}
		if err := json.Unmarshal(w.Body.Bytes(), &ids); err != nil {
			t.Fatal(err)
		}
		if len(ids) != tc.count {
			t.Fatalf("list count=%d want=%d", len(ids), tc.count)
		}
	}

	w := request("GET", "/files/"+m.FileID+"/info", aliceToken)
	var info metadata.FileAccess
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.Filename != "alice-name.pdf" {
		t.Fatalf("user received another user's filename: %q", info.Filename)
	}

	if err := store.RevokeAccessKey(ctx, aliceKey.ID); err != nil {
		t.Fatal(err)
	}
	if w := request("GET", "/registrations/"+m.FileID, aliceToken); w.Code != 401 {
		t.Fatalf("revoked credential accepted: %d", w.Code)
	}
}
