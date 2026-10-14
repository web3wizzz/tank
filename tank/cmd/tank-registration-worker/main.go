package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"tank.local/tank/internal/metadata"
	"tank.local/tank/internal/registrationguard"
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

	store, err := metadata.OpenExisting(ctx, path)
	if err != nil {
		return err
	}
	defer store.Close()
	// Fence the entire unlocked local send process, not just individual job leases.
	lane, err := filepath.Abs(path + ".local-registration.lock")
	if err != nil {
		return err
	}
	guard, err := registrationguard.Acquire(lane)
	if err != nil {
		return err
	}
	defer guard.Close()

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
