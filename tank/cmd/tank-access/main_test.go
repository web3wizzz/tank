package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
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

func TestExistingAuthorityCommandsDoNotBootstrapMissingDatabase(t *testing.T) {
	for _, command := range []string{"users", "keys", "issue", "revoke"} {
		t.Run(command, func(t *testing.T) {
			dir := t.TempDir()
			database := filepath.Join(dir, "missing", "tank.sqlite")
			output := filepath.Join(dir, "credential.json")
			args := []string{command, "--db", database}
			switch command {
			case "keys":
				args = append(args, "--user-id", "synthetic-user")
			case "issue":
				args = append(args, "--user-id", "synthetic-user", "--out", output)
			case "revoke":
				args = append(args, "--key-id", "synthetic-key")
			}
			var printed bytes.Buffer
			if err := runWithOutput(args, &printed); err == nil {
				t.Fatal("missing authority accepted")
			}
			if printed.Len() != 0 {
				t.Fatal("missing authority printed success/list output")
			}
			if _, err := os.Lstat(filepath.Dir(database)); !os.IsNotExist(err) {
				t.Fatal("administrative command created database parent")
			}
			if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Fatal("missing authority created credential output")
			}
		})
	}
}

func TestExistingAuthorityCommandsPreserveUninitializedDatabase(t *testing.T) {
	for _, command := range []string{"users", "keys", "issue", "revoke"} {
		t.Run(command, func(t *testing.T) {
			dir := t.TempDir()
			database := filepath.Join(dir, "empty.sqlite")
			output := filepath.Join(dir, "credential.json")
			if err := os.WriteFile(database, nil, 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{command, "--db", database}
			switch command {
			case "keys":
				args = append(args, "--user-id", "synthetic-user")
			case "issue":
				args = append(args, "--user-id", "synthetic-user", "--out", output)
			case "revoke":
				args = append(args, "--key-id", "synthetic-key")
			}
			var printed bytes.Buffer
			if err := runWithOutput(args, &printed); err == nil || printed.Len() != 0 {
				t.Fatal("schema-zero authority accepted")
			}
			body, err := os.ReadFile(database)
			if err != nil || len(body) != 0 {
				t.Fatal("schema-zero authority was initialized", err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 {
				t.Fatal("refused authority created sidecars/credentials", err)
			}
		})
	}
}

func TestCredentialPublicationFailureRevokesOnlyNewKey(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := metadata.Open(ctx, filepath.Join(dir, "tank.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	original, originalToken, err := store.CreateAccessKey(ctx, "synthetic publisher")
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(dir, "new", "nested", "credentials")
	moved := filepath.Join(dir, "moved-credentials")
	var replacement metadata.AccessKey
	var replacementToken string
	_, err = saveIssuedCredential(ctx, store, filepath.Join(parent, "new.json"), func(ctx context.Context) (metadata.AccessKey, string, error) {
		var issueErr error
		replacement, replacementToken, issueErr = store.IssueAccessKey(ctx, original.PrincipalID)
		if issueErr != nil {
			return replacement, replacementToken, issueErr
		}
		// The descriptor remains writable, but the requested publication path
		// disappears. Publication must not acknowledge before directory sync.
		if err := os.Rename(parent, moved); err != nil {
			t.Fatal(err)
		}
		return replacement, replacementToken, nil
	})
	if err == nil {
		t.Fatal("credential publication acknowledged without its parent directory")
	}
	if _, err := store.AuthenticateAccessKey(ctx, replacementToken); !errors.Is(err, metadata.ErrUnauthorized) {
		t.Fatal("failed publication left the new credential usable")
	}
	if _, err := store.AuthenticateAccessKey(ctx, originalToken); err != nil {
		t.Fatal("failed publication revoked the original credential")
	}
	if _, err := os.Lstat(filepath.Join(moved, "new.json")); err != nil {
		t.Fatal("moved original artifact was deleted", err)
	}
}

func TestAdministrativeListingsInspectInactiveCheckpointReadOnly(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	source := filepath.Join(dir, "live.sqlite")
	store, err := metadata.Open(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	key, token, err := store.CreateAccessKey(ctx, "synthetic inactive inspection")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeAccessKey(ctx, key.ID); err != nil {
		t.Fatal(err)
	}
	bundles := []string{filepath.Join(dir, "copy-a"), filepath.Join(dir, "copy-b")}
	if _, err := metadata.CreateCheckpoints(ctx, source, bundles); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(bundles[0], "snapshot.sqlite")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	marker, err := os.ReadFile(path + ".restore-gate")
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"users", "--db", path}, {"keys", "--db", path, "--user-id", key.PrincipalID}} {
		var out bytes.Buffer
		if err := runWithOutput(args, &out); err != nil {
			t.Fatal("read-only inactive inspection failed", err)
		}
		if !bytes.Contains(out.Bytes(), []byte(key.PrincipalID)) {
			t.Fatal("expected operator metadata missing")
		}
		if bytes.Contains(out.Bytes(), []byte(token)) || bytes.Contains(out.Bytes(), []byte("token_hash")) {
			t.Fatal("listing exposed credential material")
		}
		if args[0] == "keys" {
			var keys []metadata.CredentialInfo
			if err := json.Unmarshal(out.Bytes(), &keys); err != nil || len(keys) != 1 || !keys[0].Revoked {
				t.Fatal("revocation metadata lost", err)
			}
		}
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	afterMarker, err := os.ReadFile(path + ".restore-gate")
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(before) != sha256.Sum256(after) || !bytes.Equal(marker, afterMarker) {
		t.Fatal("listing mutated inactive authority/gate")
	}
	entries, err := os.ReadDir(bundles[0])
	if err != nil || len(entries) != 3 {
		t.Fatal("listing created database sidecars", err)
	}
	if writable, err := metadata.OpenExisting(ctx, path); err == nil {
		writable.Close()
		t.Fatal("inspection activated checkpoint")
	}
}

func TestAdministrativeListingsNeverMigrateOlderSchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "schema-seven.sqlite")
	store, err := metadata.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE registration_spend_reservations; DROP TABLE registration_spend_accounts; PRAGMA user_version=7;`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"users", "--db", path}, {"keys", "--db", path, "--user-id", "synthetic-user"}} {
		var out bytes.Buffer
		if err := runWithOutput(args, &out); err == nil || out.Len() != 0 {
			t.Fatal("unsupported schema listed or migrated")
		}
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(before) != sha256.Sum256(after) {
		t.Fatal("listing changed older schema bytes")
	}
}
