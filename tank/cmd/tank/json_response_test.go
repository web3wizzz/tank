package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIJSONResponseMustBeOneBoundedValue(t *testing.T) {
	for name, body := range map[string]string{
		"second-value":     `{"ok":true} {"ok":false}`,
		"trailing-garbage": `{"ok":true} truncated`,
		"oversized-suffix": `{"ok":true}` + strings.Repeat(" ", 8<<20),
		"truncated-value":  `{"ok":`,
	} {
		t.Run(name, func(t *testing.T) {
			var value struct {
				OK bool `json:"ok"`
			}
			if err := decodeJSON(strings.NewReader(body), &value); err == nil {
				t.Fatal("ambiguous/incomplete/oversized API response accepted")
			}
		})
	}
	var value struct {
		OK bool `json:"ok"`
	}
	if err := decodeJSON(strings.NewReader("{\"ok\":true}\n \t"), &value); err != nil || !value.OK {
		t.Fatal("valid response rejected", err)
	}
}

func TestCLIUploadDoesNotPrintSuccessForAmbiguousResponse(t *testing.T) {
	t.Setenv("TANK_API_TOKEN", "synthetic-test-token")
	input := []byte("synthetic CLI upload")
	sum := sha256.Sum256(input)
	id := hex.EncodeToString(sum[:])
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		fmt.Fprintf(w, `{"file_id":%q} {"unexpected":true}`, id)
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "input")
	if err := os.WriteFile(path, input, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := rootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--api", server.URL, "tank", path})
	if err := cmd.Execute(); err == nil {
		t.Fatal("ambiguous upload response reported success")
	}
	if strings.Contains(out.String(), id) {
		t.Fatal("failure printed a success file ID")
	}
}
