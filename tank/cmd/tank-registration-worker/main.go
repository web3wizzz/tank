package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

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
	return runArgs(os.Args[1:])
}

func runArgs(args []string) error {
	flags := flag.NewFlagSet("tank-registration-worker", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	enqueueOnly := flags.Bool("enqueue-only", false, "queue missing local registrations without contacting the chain")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return errors.New("use tank-registration-worker [--enqueue-only]")
	}
	ctx, stop := signal.NotifyContext(
		context.Background(), os.Interrupt, syscall.SIGTERM,
	)
	defer stop()
	if *enqueueOnly {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}

	path := os.Getenv("TANK_DATABASE_PATH")
	if path == "" {
		path = "data/local-demo/tank.sqlite"
	}
	// Restore copies stay inactive even when the worker's lane path differs
	// from the controller's stable lane. Check before opening/migrating SQLite.
	if _, err := os.Lstat(path + ".restore-gate"); !os.IsNotExist(err) {
		return registrationguard.ErrBlocked
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

	if *enqueueOnly {
		return worker.Reconcile(ctx)
	}
	log.Print("local registration worker started")
	worker.Run(ctx)
	return nil
}
