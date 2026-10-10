package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"tank.local/tank/internal/metadata"
)

func defaultDatabase() string {
	if path := os.Getenv("TANK_DATABASE_PATH"); path != "" {
		return path
	}
	return "data/local-demo/tank.sqlite"
}

func saveCredential(
	ctx context.Context,
	store *metadata.Store,
	label, output string,
) (key metadata.AccessKey, err error) {
	return saveIssuedCredential(ctx, store, output, func(ctx context.Context) (metadata.AccessKey, string, error) {
		return store.CreateAccessKey(ctx, label)
	})
}

func saveIssuedCredential(
	ctx context.Context,
	store *metadata.Store,
	output string,
	issue func(context.Context) (metadata.AccessKey, string, error),
) (key metadata.AccessKey, err error) {
	if output == "" {
		return key, errors.New("--out is required")
	}
	if err := os.MkdirAll(filepath.Dir(output), 0700); err != nil {
		return key, err
	}

	// Refuse to overwrite existing files, including symlinks.
	file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return key, err
	}

	saved := false
	defer func() {
		if saved {
			return
		}
		file.Close()
		os.Remove(output)

		if key.ID != "" {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if revokeErr := store.RevokeAccessKey(cleanupCtx, key.ID); revokeErr != nil {
				err = fmt.Errorf(
					"%w; revoke credential %s manually: %v",
					err, key.ID, revokeErr,
				)
			}
		}
	}()

	var token string
	key, token, err = issue(ctx)
	if err != nil {
		return key, err
	}

	record := struct {
		metadata.AccessKey
		Token string `json:"token"`
	}{key, token}

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(record); err != nil {
		return key, err
	}
	if err := file.Sync(); err != nil {
		return key, err
	}
	if err := file.Close(); err != nil {
		return key, err
	}

	saved = true
	return key, nil
}

func run(args []string) error {
	return runWithOutput(args, os.Stdout)
}

func runWithOutput(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: tank-access create|issue|users|keys|revoke [flags]")
	}

	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	database := flags.String("db", defaultDatabase(), "SQLite database path")

	var label, output, keyID, userID *string
	switch args[0] {
	case "create":
		label = flags.String("label", "", "User label")
		output = flags.String("out", "", "Private credential file; must not exist")
	case "issue":
		userID = flags.String("user-id", "", "Existing user ID; file permissions are preserved")
		output = flags.String("out", "", "New private credential file; must not exist")
	case "users":
		// Local administrative listing; no credential material is returned.
	case "keys":
		userID = flags.String("user-id", "", "Existing user ID")
	case "revoke":
		keyID = flags.String("key-id", "", "Credential ID to revoke")
	default:
		return errors.New("expected create, issue, users, keys, or revoke")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if args[0] == "create" && (*label == "" || *output == "") {
		return errors.New("create requires --label and --out")
	}
	if args[0] == "issue" && (*userID == "" || *output == "") {
		return errors.New("issue requires --user-id and --out")
	}
	if args[0] == "keys" && *userID == "" {
		return errors.New("keys requires --user-id")
	}
	if args[0] == "revoke" && *keyID == "" {
		return errors.New("revoke requires --key-id")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store, err := metadata.Open(ctx, *database)
	if err != nil {
		return err
	}
	defer store.Close()

	switch args[0] {
	case "create", "issue":
		var key metadata.AccessKey
		var err error
		if args[0] == "create" {
			key, err = saveCredential(ctx, store, *label, *output)
		} else {
			key, err = saveIssuedCredential(ctx, store, *output, func(ctx context.Context) (metadata.AccessKey, string, error) {
				return store.IssueAccessKey(ctx, *userID)
			})
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Credential saved: %s\n", *output)
		fmt.Fprintf(out, "User: %s\nCredential ID: %s\nExpires: %s\n",
			key.PrincipalID, key.ID, key.ExpiresAt.Format(time.RFC3339))
	case "users":
		users, err := store.ListPrincipals(ctx)
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(users)
	case "keys":
		keys, err := store.ListAccessKeys(ctx, *userID)
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(keys)
	case "revoke":
		if err := store.RevokeAccessKey(ctx, *keyID); err != nil {
			return err
		}
		fmt.Fprintf(out, "Credential revoked: %s\n", *keyID)
	}
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "Tank access:", err)
		os.Exit(1)
	}
}
