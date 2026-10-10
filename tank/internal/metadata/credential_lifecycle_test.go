package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReplacementCredentialsPreserveUserAndFiles(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tank.sqlite")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	original, originalToken, err := store.CreateAccessKey(ctx, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	bob, _, err := store.CreateAccessKey(ctx, "Bob")
	if err != nil {
		t.Fatal(err)
	}
	manifest := filenameTestManifest(t)
	if err := store.Save(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	if err := store.GrantFileAccess(ctx, original.PrincipalID, manifest.FileID, "private.pdf"); err != nil {
		t.Fatal(err)
	}
	replacement, replacementToken, err := store.IssueAccessKey(ctx, original.PrincipalID)
	if err != nil {
		t.Fatal(err)
	}
	if replacement.PrincipalID != original.PrincipalID || replacement.ID == original.ID || replacementToken == originalToken {
		t.Fatal("replacement must be a fresh credential for the existing user")
	}
	remaining := time.Until(replacement.ExpiresAt)
	if remaining < 30*24*time.Hour-time.Minute || remaining > 30*24*time.Hour {
		t.Fatal("replacement expiry must be 30 days")
	}
	for _, token := range []string{originalToken, replacementToken} {
		user, err := store.AuthenticateAccessKey(ctx, token)
		if err != nil || user.ID != original.PrincipalID {
			t.Fatal("credential did not authenticate the original user")
		}
		access, err := store.LoadFileAccess(ctx, user.ID, manifest.FileID)
		if err != nil || access.Filename != "private.pdf" {
			t.Fatal("replacement lost original file permissions or filename")
		}
	}
	if _, err := store.LoadFileAccess(ctx, bob.PrincipalID, manifest.FileID); !errors.Is(err, ErrAccessDenied) {
		t.Fatal("issuing a replacement leaked access to another user")
	}
	if _, err := store.db.ExecContext(ctx, "UPDATE api_credentials SET expires_at = 1 WHERE id = ?", original.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AuthenticateAccessKey(ctx, originalToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("expired original credential was accepted")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AuthenticateAccessKey(ctx, replacementToken); err != nil {
		t.Fatal("replacement did not survive database reopen")
	}
	if err := store.RevokeAccessKey(ctx, replacement.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AuthenticateAccessKey(ctx, replacementToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("revoked replacement was accepted")
	}
	recovered, recoveredToken, err := store.IssueAccessKey(ctx, original.PrincipalID)
	if err != nil || recovered.PrincipalID != original.PrincipalID {
		t.Fatal("administrator cannot recover an existing user after all keys expire or are revoked")
	}
	user, err := store.AuthenticateAccessKey(ctx, recoveredToken)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := store.ListAccessibleFiles(ctx, user.ID, "")
	if err != nil || len(ids) != 1 || ids[0] != manifest.FileID {
		t.Fatal("credential recovery changed file access")
	}
}

func TestCredentialListingsNeverExposeSecretsAndUnknownUsersAreRejected(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "tank.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	key, token, err := store.CreateAccessKey(ctx, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	other, otherToken, err := store.IssueAccessKey(ctx, key.PrincipalID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeAccessKey(ctx, key.ID); err != nil {
		t.Fatal(err)
	}
	users, err := store.ListPrincipals(ctx)
	if err != nil || len(users) != 1 || users[0].ID != key.PrincipalID || users[0].Label != "Alice" {
		t.Fatal("incorrect user metadata")
	}
	keys, err := store.ListAccessKeys(ctx, key.PrincipalID)
	if err != nil || len(keys) != 2 {
		t.Fatal("incorrect credential metadata")
	}
	for _, info := range keys {
		if info.PrincipalID != key.PrincipalID || info.Revoked != (info.ID == key.ID) {
			t.Fatal("incorrect credential revocation status")
		}
		if info.ID != key.ID && info.ID != other.ID {
			t.Fatal("unexpected credential")
		}
	}
	encoded, err := json.Marshal(keys)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), token) || strings.Contains(string(encoded), otherToken) || strings.Contains(string(encoded), "token_hash") || strings.Contains(string(encoded), `"token"`) {
		t.Fatal("credential metadata exposed secret material")
	}
	missing := "usr_" + strings.Repeat("0", 32)
	for _, id := range []string{missing, "", "' OR 1=1 --"} {
		record, secret, err := store.IssueAccessKey(ctx, id)
		if !errors.Is(err, ErrPrincipalNotFound) || record.ID != "" || secret != "" {
			t.Fatal("unknown user received a credential")
		}
		if _, err := store.ListAccessKeys(ctx, id); !errors.Is(err, ErrPrincipalNotFound) {
			t.Fatal("unknown user listing succeeded")
		}
	}
	keys, err = store.ListAccessKeys(ctx, key.PrincipalID)
	if err != nil || len(keys) != 2 {
		t.Fatal("failed issuance changed existing credentials")
	}
}
