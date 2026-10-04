package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"tank.local/tank/internal/metadata"
	"tank.local/tank/internal/registry"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(
		context.Background(), os.Interrupt, syscall.SIGTERM,
	)
	defer stop()

	path := os.Getenv("TANK_DATABASE_PATH")
	if path == "" {
		path = "data/local-demo/tank.sqlite"
	}

	store, err := metadata.Open(ctx, path)
	if err != nil {
		return err
	}
	defer store.Close()

	worker, err := registry.NewWorker(store, registry.Config{
		RPCURL:     os.Getenv("TANK_CHAIN_RPC"),
		Contract:   os.Getenv("TANK_REGISTRY_ADDR"),
		Registrant: os.Getenv("TANK_REGISTRANT"),
	})
	if err != nil {
		return err
	}

	log.Print("local registration worker started")
	worker.Run(ctx)
	return nil
}
