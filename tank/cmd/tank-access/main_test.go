package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"tank.local/tank/internal/metadata"
)

type privateRecord struct {
	metadata.AccessKey
	Token string `json:"token"`
}

func readPrivateRecord(t *testing.T, path string) privateRecord {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record privateRecord
	if err := json.Unmarshal(body, &record); err != nil {
		t.Fatal("invalid private credential JSON")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("credential file must be private")
	}
	return record
}

func TestCLIIssuesReplacementAndListsOnlyMetadata(t *testing.T) {
	directory := t.TempDir()
	database := filepath.Join(directory, "tank.sqlite")
	originalPath := filepath.Join(directory, "original.json")
	replacementPath := filepath.Join(directory, "replacement.json")
	var output bytes.Buffer
	if err := runWithOutput([]string{"create", "--db", database, "--label", "Alice", "--out", originalPath}, &output); err != nil {
		t.Fatal(err)
	}
	original := readPrivateRecord(t, originalPath)
	if err := runWithOutput([]string{"issue", "--db", database, "--user-id", original.PrincipalID, "--out", replacementPath}, &output); err != nil {
		t.Fatal(err)
	}
	replacement := readPrivateRecord(t, replacementPath)
	if replacement.PrincipalID != original.PrincipalID || replacement.ID == original.ID {
		t.Fatal("CLI replacement changed the user or reused a credential")
	}
	for _, args := range [][]string{
		{"users", "--db", database},
		{"keys", "--db", database, "--user-id", original.PrincipalID},
		{"revoke", "--db", database, "--key-id", original.ID},
	} {
		if err := runWithOutput(args, &output); err != nil {
			t.Fatal(err)
		}
	}
	if bytes.Contains(output.Bytes(), []byte(original.Token)) || bytes.Contains(output.Bytes(), []byte(replacement.Token)) || bytes.Contains(output.Bytes(), []byte("token_hash")) {
		t.Fatal("CLI printed secret material")
	}
	store, err := metadata.Open(context.Background(), database)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.AuthenticateAccessKey(context.Background(), original.Token); !errors.Is(err, metadata.ErrUnauthorized) {
		t.Fatal("revoked original credential still works")
	}
	user, err := store.AuthenticateAccessKey(context.Background(), replacement.Token)
	if err != nil || user.ID != original.PrincipalID {
		t.Fatal("revoking the original invalidated its replacement")
	}
}

func TestCredentialSaveRefusesExistingFilesAndFailedIssuancePreservesKeys(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	store, err := metadata.Open(ctx, filepath.Join(directory, "tank.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	original, err := saveCredential(ctx, store, "Alice", filepath.Join(directory, "original.json"))
	if err != nil {
		t.Fatal(err)
	}
	issue := func(ctx context.Context) (metadata.AccessKey, string, error) {
		return store.IssueAccessKey(ctx, original.PrincipalID)
	}
	target := filepath.Join(directory, "existing")
	if err := os.WriteFile(target, []byte("preserve existing data"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "linked")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{target, link} {
		if _, err := saveIssuedCredential(ctx, store, path, issue); err == nil {
			t.Fatal("existing destination was overwritten")
		}
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "preserve existing data" {
		t.Fatal("existing data changed")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	failedPath := filepath.Join(directory, "failed.json")
	if _, err := saveIssuedCredential(canceled, store, failedPath, issue); err == nil {
		t.Fatal("canceled issuance succeeded")
	}
	if _, err := os.Stat(failedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed issuance left an incomplete private credential file")
	}
	missingPath := filepath.Join(directory, "unknown.json")
	if _, err := saveIssuedCredential(ctx, store, missingPath, func(ctx context.Context) (metadata.AccessKey, string, error) {
		return store.IssueAccessKey(ctx, "missing")
	}); !errors.Is(err, metadata.ErrPrincipalNotFound) {
		t.Fatal("unknown user issuance succeeded")
	}
	if _, err := os.Stat(missingPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unknown user issuance left a private file")
	}
	keys, err := store.ListAccessKeys(ctx, original.PrincipalID)
	if err != nil || len(keys) != 1 || keys[0].Revoked {
		t.Fatal("failed issuance changed existing credentials")
	}
}
