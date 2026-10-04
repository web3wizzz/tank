package tank

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func contentID(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func newTestClient(t *testing.T, address, token string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: address, Token: token})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestClientWorkflow(t *testing.T) {
	data := []byte("Tank SDK round trip")
	id := contentID(data)
	token := "test-token"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			if r.Header.Get("Authorization") != "" {
				t.Error("health should not send a token")
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		switch r.Method + " " + r.URL.Path {
		case "POST /tank":
			body, err := io.ReadAll(r.Body)
			if err != nil || !bytes.Equal(body, data) {
				t.Error("incorrect request body")
			}
			if r.Header.Get("Content-Type") != "application/octet-stream" {
				t.Error("incorrect content type")
			}
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(Manifest{
				Version: 1, FileID: id, Size: int64(len(data)),
			})
		case "GET /retrieve/" + id:
			w.Write(data)
		case "GET /list":
			if r.URL.Query().Get("after") != id {
				t.Error("cursor was not sent")
			}
			json.NewEncoder(w).Encode([]string{id})
		case "GET /registrations/" + id:
			json.NewEncoder(w).Encode(Registration{
				FileID: id, Target: "local-test",
				Status: "registered", Attempts: 1,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, token)
	ctx := context.Background()

	if err := client.Health(ctx); err != nil {
		t.Fatal(err)
	}
	manifest, err := client.Tank(ctx, data)
	if err != nil || manifest.FileID != id {
		t.Fatalf("tank: manifest=%+v err=%v", manifest, err)
	}
	got, err := client.Retrieve(ctx, id)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("retrieve: data=%q err=%v", got, err)
	}
	ids, err := client.List(ctx, id)
	if err != nil || len(ids) != 1 || ids[0] != id {
		t.Fatalf("list: ids=%v err=%v", ids, err)
	}
	status, err := client.RegistrationStatus(ctx, id)
	if err != nil || status.Status != "registered" {
		t.Fatalf("registration: status=%+v err=%v", status, err)
	}
}

func TestRetrieveRejectsCorruption(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("corrupted bytes"))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, "token")
	data, err := client.Retrieve(context.Background(), contentID([]byte("original bytes")))
	if !errors.Is(err, ErrIntegrity) || data != nil {
		t.Fatalf("corrupted bytes were returned: data=%q err=%v", data, err)
	}
}

func TestTankRejectsMismatchedID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		json.NewEncoder(w).Encode(Manifest{
			Version: 1, FileID: contentID([]byte("other")), Size: 4,
		})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, "token")
	_, err := client.Tank(context.Background(), []byte("sent"))
	if !errors.Is(err, ErrIntegrity) {
		t.Fatalf("expected integrity error, got %v", err)
	}
}

func TestRedirectIsNotFollowed(t *testing.T) {
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	client := newTestClient(t, source.URL, "private-token")
	_, err := client.Retrieve(context.Background(), contentID([]byte("file")))
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("expected redirect HTTP error, got %v", err)
	}
	if calls.Load() != 0 {
		t.Fatal("redirect destination was contacted")
	}
}

func TestAPIErrorAndCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, "token")
	err := client.Health(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected typed HTTP error, got %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client.Health(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestInvalidInputMakesNoRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, "token")

	if _, err := client.Tank(context.Background(), nil); !errors.Is(err, ErrInvalidSize) {
		t.Fatalf("empty input: %v", err)
	}
	if _, err := client.Tank(context.Background(), make([]byte, MaxFileBytes+1)); !errors.Is(err, ErrInvalidSize) {
		t.Fatalf("oversized input: %v", err)
	}
	if _, err := client.Retrieve(context.Background(), "../file"); !errors.Is(err, ErrInvalidFileID) {
		t.Fatalf("invalid ID: %v", err)
	}
	if _, err := client.List(context.Background(), "bad"); !errors.Is(err, ErrInvalidFileID) {
		t.Fatalf("invalid cursor: %v", err)
	}
	if _, err := client.RegistrationStatus(context.Background(), "bad"); !errors.Is(err, ErrInvalidFileID) {
		t.Fatalf("invalid registration ID: %v", err)
	}
	if calls.Load() != 0 {
		t.Fatal("invalid input caused an HTTP request")
	}
}

func TestRejectsTrailingJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "[] []")
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, "token")
	if _, err := client.List(context.Background(), ""); err == nil {
		t.Fatal("accepted multiple JSON values")
	}
}
