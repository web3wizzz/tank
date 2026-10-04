package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"tank.local/tank/internal/integrity"
	"tank.local/tank/internal/metadata"
	"tank.local/tank/internal/registry"
)

func run() error {
	if len(os.Args) > 2 {
		return fmt.Errorf("usage: tank-register [FILE_ID]")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	path := os.Getenv("TANK_DATABASE_PATH")
	if path == "" {
		path = "data/local-demo/tank.sqlite"
	}

	store, err := metadata.Open(ctx, path)
	if err != nil {
		return err
	}
	defer store.Close()

	var id string
	if len(os.Args) == 2 {
		id = os.Args[1]
	} else {
		ids, err := store.List(ctx, "", 1)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return fmt.Errorf("no stored files found; tank a file first")
		}
		id = ids[0]
	}

	manifest, err := store.Load(ctx, id)
	if err != nil {
		return err
	}

	client, err := registry.Open(ctx, registry.Config{
		RPCURL:     os.Getenv("TANK_CHAIN_RPC"),
		Contract:   os.Getenv("TANK_REGISTRY_ADDR"),
		Registrant: os.Getenv("TANK_REGISTRANT"),
	})
	if err != nil {
		return err
	}
	defer client.Close()

	transaction, err := client.Register(ctx, manifest)
	if err != nil {
		return err
	}

	if transaction == (common.Hash{}) {
		fmt.Println("Identical registration already exists.")
	} else {
		fmt.Println("Transaction:", transaction.Hex())
	}

	if err := client.Wait(ctx, transaction); err != nil {
		return err
	}

	fileID, err := integrity.ParseHash(id)
	if err != nil {
		return err
	}

	record, found, err := client.Get(ctx, fileID)
	if err != nil {
		return err
	}

	expected, err := manifest.Commitment()
	if err != nil {
		return err
	}
	if !found || record.Commitment != expected ||
		record.FileSize != uint64(manifest.Size) {
		return fmt.Errorf("on-chain record does not match the stored manifest")
	}

	fmt.Println("File ID:", id)
	fmt.Println("Commitment:", record.Commitment.String())
	fmt.Println("PASS: on-chain commitment matches the stored file manifest.")
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "[Tank]", err)
		os.Exit(1)
	}
}
