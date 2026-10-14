package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"tank.local/tank/pkg/tank"
	"testing"
)

func TestEncryptedCLI(t *testing.T) {
	t.Setenv("TANK_API_TOKEN", "test-token")
	dir := t.TempDir()
	input := filepath.Join(dir, "Résumé.pdf")
	recoveryPath := filepath.Join(dir, "recovery.json")
	original := []byte("CLI private input")
	if err := os.WriteFile(input, original, 0600); err != nil {
		t.Fatal(err)
	}
	var stored []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			secret, err := os.ReadFile(recoveryPath)
			if err != nil {
				t.Error("recovery was not persisted before upload")
				w.WriteHeader(500)
				return
			}
			defer clear(secret)
			record, err := tank.ParseRecovery(secret)
			if err != nil {
				t.Error("invalid saved recovery")
				w.WriteHeader(500)
				return
			}
			stored, err = io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			sum := sha256.Sum256(stored)
			if hex.EncodeToString(sum[:]) != record.FileID {
				t.Error("recovery ID does not match ciphertext")
			}
			if r.Header.Get("X-Tank-Filename") != "" {
				t.Error("encrypted filename sent as plaintext")
			}
			json.NewEncoder(w).Encode(map[string]string{"file_id": record.FileID})
			return
		}
		w.Write(stored)
	}))
	defer server.Close()
	run := func(args ...string) (string, error) {
		cmd := rootCommand()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(append([]string{"--api", server.URL}, args...))
		err := cmd.Execute()
		return out.String(), err
	}
	printed, err := run("tank", input, "--encrypt", "--recovery-out", recoveryPath)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := os.ReadFile(recoveryPath)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(secret)
	record, err := tank.ParseRecovery(secret)
	if err != nil {
		t.Fatal(err)
	}
	if printed != record.FileID+"\n" || strings.Contains(printed, record.Key) {
		t.Fatal("CLI output must contain only ciphertext ID")
	}
	info, err := os.Stat(recoveryPath)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("recovery must be private")
	}
	output := filepath.Join(dir, "output.pdf")
	if _, err = run("retrieve", record.FileID, "--out", output, "--recovery", recoveryPath); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil || !bytes.Equal(data, original) {
		t.Fatal("decrypted output mismatch")
	}
	if _, err = run("retrieve", record.FileID, "--out", output, "--recovery", recoveryPath); err == nil {
		t.Fatal("output overwritten")
	}
	stored[len(stored)-1] ^= 1
	damaged := filepath.Join(dir, "damaged.pdf")
	if _, err = run("retrieve", record.FileID, "--out", damaged, "--recovery", recoveryPath); err == nil {
		t.Fatal("damage accepted")
	}
	if _, err = os.Stat(damaged); !os.IsNotExist(err) {
		t.Fatal("damaged plaintext published")
	}
	if _, err = run("tank", input, "--encrypt", "--recovery-out", recoveryPath); err == nil {
		t.Fatal("recovery overwritten")
	}
}

func TestEncryptedUploadFailurePreservesRecovery(t *testing.T) {
	t.Setenv("TANK_API_TOKEN", "test-token")
	dir := t.TempDir()
	input := filepath.Join(dir, "input")
	recovery := filepath.Join(dir, "recovery.json")
	if err := os.WriteFile(input, []byte("input"), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer server.Close()
	cmd := rootCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--api", server.URL, "tank", input, "--encrypt", "--recovery-out", recovery})
	if err := cmd.Execute(); err == nil {
		t.Fatal("upload should fail")
	}
	data, err := os.ReadFile(recovery)
	if err != nil {
		t.Fatal("uncertain upload lost recovery")
	}
	defer clear(data)
	if _, err = tank.ParseRecovery(data); err != nil {
		t.Fatal("invalid preserved recovery")
	}
}
