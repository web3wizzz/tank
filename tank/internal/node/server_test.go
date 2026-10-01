package node

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tank.local/tank/internal/integrity"
	"tank.local/tank/internal/storage"
)

func TestAuthenticatedStorageAndRetrieval(t *testing.T) {
	backend, err := storage.NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()

	token := strings.Repeat("x", 32)
	handler, err := NewHandler(backend, token)
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(handler)
	defer server.Close()

	data := []byte("Tank network shard")
	hash := integrity.Digest(data).String()
	id := integrity.Digest([]byte("whole file")).String()
	url := server.URL + "/shards/" + id + "/0/0"

	request := func(method, auth, claimedHash string, body []byte) (int, []byte) {
		t.Helper()

		req, err := http.NewRequest(method, url, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if auth != "" {
			req.Header.Set("Authorization", "Bearer "+auth)
		}
		if claimedHash != "" {
			req.Header.Set("X-Tank-Shard-Hash", claimedHash)
		}

		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()

		got, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		return res.StatusCode, got
	}

	if code, _ := request("PUT", "", hash, data); code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated PUT returned %d", code)
	}

	if code, _ := request("GET", token, "", nil); code != http.StatusNotFound {
		t.Fatalf("missing shard returned %d", code)
	}

	wrongHash := integrity.Digest([]byte("wrong")).String()
	if code, _ := request("PUT", token, wrongHash, data); code != http.StatusUnprocessableEntity {
		t.Fatalf("incorrect hash returned %d", code)
	}

	if code, _ := request("PUT", token, hash, data); code != http.StatusCreated {
		t.Fatalf("PUT returned %d", code)
	}

	if code, _ := request("GET", "wrong-token", "", nil); code != http.StatusUnauthorized {
		t.Fatalf("wrong token returned %d", code)
	}

	code, got := request("GET", token, "", nil)
	if code != http.StatusOK || !bytes.Equal(got, data) {
		t.Fatalf("GET returned %d with incorrect data", code)
	}
}
