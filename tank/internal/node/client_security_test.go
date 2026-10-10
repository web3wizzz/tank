package node

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"tank.local/tank/internal/integrity"
	"tank.local/tank/internal/storage"
	"testing"
)

func TestRemoteNodesRequireHTTPS(t *testing.T) {
	for _, address := range []string{"http://example.invalid", "http://10.0.0.1:9101", "http://127.0.0.1.example.invalid", "http://[2001:db8::1]"} {
		if _, err := NewClient(address, strings.Repeat("n", 32)); err == nil {
			t.Fatal("remote plaintext node accepted")
		}
	}
	for _, address := range []string{"http://localhost:9101", "http://127.0.0.1:9101", "http://[::1]:9101", "https://node.example.invalid"} {
		if _, err := NewClient(address, strings.Repeat("n", 32)); err != nil {
			t.Fatal(err)
		}
	}
}
func TestTLSValidationAndRedirectRefusal(t *testing.T) {
	token := strings.Repeat("n", 32)
	data := []byte("verified TLS shard")
	key := storage.ShardKey{FileID: integrity.Digest(data).String()}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(401)
			return
		}
		w.Write(data)
	}))
	defer server.Close()
	client, err := NewClient(server.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Get(context.Background(), key, integrity.Digest(data)); err == nil {
		t.Fatal("untrusted TLS certificate accepted")
	}
	client.http.Transport = server.Client().Transport
	if got, err := client.Get(context.Background(), key, integrity.Digest(data)); err != nil || string(got) != string(data) {
		t.Fatal("trusted TLS failed verified retrieval")
	}
	reached := false
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	defer destination.Close()
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 302) }))
	defer redirect.Close()
	client, err = NewClient(redirect.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	client.http.Transport = redirect.Client().Transport
	if _, err := client.Get(context.Background(), key, integrity.Digest(data)); err == nil {
		t.Fatal("redirect accepted")
	}
	if reached {
		t.Fatal("node credential redirect destination contacted")
	}
}
func TestConfiguredNodeCredentialsArePrivateDistinctAndNotPrinted(t *testing.T) {
	addresses := []string{"http://127.0.0.1:9101", "http://127.0.0.1:9102", "http://127.0.0.1:9103", "http://127.0.0.1:9104"}
	credentials := map[string]string{}
	for i, address := range addresses {
		credentials[address] = strings.Repeat(string(rune('a'+i)), 32)
	}
	file := filepath.Join(t.TempDir(), "node-credentials.json")
	bytes, _ := json.Marshal(credentials)
	if err := os.WriteFile(file, bytes, 0600); err != nil {
		t.Fatal(err)
	}
	clients, err := ConfiguredClients(strings.Join(addresses, ","), "", file)
	if err != nil {
		t.Fatal(err)
	}
	for i, client := range clients {
		if client.token != credentials[addresses[i]] || client.BaseURL != addresses[i] {
			t.Fatal("node credential or placement order changed")
		}
	}
	os.Chmod(file, 0644)
	if _, err := ConfiguredClients(strings.Join(addresses, ","), "", file); err == nil {
		t.Fatal("world-readable credential map accepted")
	}
	os.Chmod(file, 0600)
	link := file + ".link"
	os.Symlink(file, link)
	if _, err := ConfiguredClients(strings.Join(addresses, ","), "", link); err == nil {
		t.Fatal("credential symlink accepted")
	}
	credentials[addresses[1]] = credentials[addresses[0]]
	bytes, _ = json.Marshal(credentials)
	os.WriteFile(file, bytes, 0600)
	if _, err := ConfiguredClients(strings.Join(addresses, ","), "", file); err == nil {
		t.Fatal("duplicate per-node credentials accepted")
	}
	credentials[addresses[1]] = strings.Repeat("b", 32)
	delete(credentials, addresses[0])
	bytes, _ = json.Marshal(credentials)
	os.WriteFile(file, bytes, 0600)
	if _, err := ConfiguredClients(strings.Join(addresses, ","), "", file); err == nil {
		t.Fatal("incomplete credential map accepted")
	}
	if _, err := ConfiguredClients(strings.Join(addresses, ","), strings.Repeat("n", 32), ""); err != nil {
		t.Fatal("shared local token compatibility lost")
	}
}
