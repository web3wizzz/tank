package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"tank.local/tank/internal/config"
	"tank.local/tank/internal/coordinator"
	"tank.local/tank/internal/metadata"
	"tank.local/tank/internal/node"
)

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	store, err := metadata.Open(context.Background(), cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer store.Close()

	var clients []*node.Client
	for _, address := range strings.Split(os.Getenv("TANK_NODES"), ",") {
		client, err := node.NewClient(
			strings.TrimSpace(address),
			os.Getenv("TANK_NODE_TOKEN"),
		)
		if err != nil {
			return err
		}
		clients = append(clients, client)
	}

	service, err := coordinator.NewService(store, clients, cfg.MaxSegmentBytes)
	if err != nil {
		return err
	}

	handler, err := coordinator.NewHandlerWithRegistration(
		service,
		store,
		os.Getenv("TANK_API_TOKEN"),
		os.Getenv("TANK_CHAIN_RPC"),
		os.Getenv("TANK_REGISTRY_ADDR"),
		os.Getenv("TANK_REGISTRANT"),
	)
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              cfg.CoordinatorAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	ctx, stop := signal.NotifyContext(
		context.Background(), os.Interrupt, syscall.SIGTERM,
	)
	defer stop()

	maintenanceDone := make(chan struct{})
	go func() {
		defer close(maintenanceDone)
		service.RunMaintenance(ctx)
	}()
	defer func() {
		stop()
		<-maintenanceDone
	}()

	result := make(chan error, 1)
	go func() {
		log.Printf("[Tank] coordinator listening on %s", cfg.CoordinatorAddr)
		result <- server.ListenAndServe()
	}()

	select {
	case err := <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			server.Close()
			return err
		}
		return nil
	}
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
