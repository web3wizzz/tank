package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"tank.local/tank/internal/config"
	"tank.local/tank/internal/coordinator"
	"tank.local/tank/internal/limits"
	"tank.local/tank/internal/metadata"
	"tank.local/tank/internal/node"
)

func registrationHandler(service *coordinator.Service, store *metadata.Store) (http.Handler, error) {
	switch os.Getenv("TANK_REGISTRATION_MODE") {
	case "", "local":
		return coordinator.NewHandlerWithRegistration(service, store, os.Getenv("TANK_API_TOKEN"), os.Getenv("TANK_CHAIN_RPC"), os.Getenv("TANK_REGISTRY_ADDR"), os.Getenv("TANK_REGISTRANT"))
	case "arc-testnet-external":
		chainID, err := strconv.ParseInt(os.Getenv("TANK_CHAIN_ID"), 10, 64)
		if err != nil {
			return nil, errors.New("external Arc registration requires explicit testnet chain ID")
		}
		return coordinator.NewHandlerWithExternalArcRegistration(service, store, os.Getenv("TANK_API_TOKEN"), chainID, os.Getenv("TANK_REGISTRY_ADDR"), os.Getenv("TANK_ARC_TESTNET_SENDER"))
	default:
		return nil, errors.New("unknown registration mode")
	}
}

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

	clients, err := node.ConfiguredClients(os.Getenv("TANK_NODES"), os.Getenv("TANK_NODE_TOKEN"), os.Getenv("TANK_NODE_CREDENTIALS_FILE"))
	if err != nil {
		return err
	}

	service, err := coordinator.NewServiceWithLimits(store, clients, cfg.MaxSegmentBytes, cfg.Limits)
	if err != nil {
		return err
	}

	handler, err := registrationHandler(service, store)
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              cfg.CoordinatorAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       min(30*time.Second, cfg.Limits.RequestTimeout),
		WriteTimeout:      cfg.Limits.RequestTimeout + 5*time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    cfg.Limits.MaxHeaderBytes,
	}

	listener, err := limits.Listen(cfg.CoordinatorAddr, cfg.Limits.MaxConnections)
	if err != nil {
		return err
	}
	defer listener.Close()

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
		result <- server.Serve(listener)
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
