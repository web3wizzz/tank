package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
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
	key, token, err = store.CreateAccessKey(ctx, label)
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
	if len(args) == 0 {
		return errors.New("usage: tank-access create|revoke [flags]")
	}

	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	database := flags.String("db", defaultDatabase(), "SQLite database path")

	var label, output, keyID *string
	switch args[0] {
	case "create":
		label = flags.String("label", "", "User label")
		output = flags.String("out", "", "Private credential file; must not exist")
	case "revoke":
		keyID = flags.String("key-id", "", "Credential ID to revoke")
	default:
		return errors.New("expected create or revoke")
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
	case "create":
		key, err := saveCredential(ctx, store, *label, *output)
		if err != nil {
			return err
		}
		fmt.Printf("Credential saved: %s\n", *output)
		fmt.Printf("User: %s\nCredential ID: %s\nExpires: %s\n",
			key.PrincipalID, key.ID, key.ExpiresAt.Format(time.RFC3339))
	case "revoke":
		if err := store.RevokeAccessKey(ctx, *keyID); err != nil {
			return err
		}
		fmt.Printf("Credential revoked: %s\n", *keyID)
	}
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "Tank access:", err)
		os.Exit(1)
	}
}
