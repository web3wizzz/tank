package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tank.local/tank/internal/config"
	"tank.local/tank/internal/limits"
	"tank.local/tank/internal/node"
	"tank.local/tank/internal/storage"
)

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	backend, err := storage.NewFilesystemWithQuota(cfg.NodeDataDir, cfg.Limits.NodeStorageBytes, cfg.Limits.NodeFileLimit)
	if err != nil {
		return err
	}
	defer backend.Close()

	handler, err := node.NewHandlerWithLimits(backend, os.Getenv("TANK_NODE_TOKEN"), cfg.Limits)
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              cfg.NodeAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       min(30*time.Second, cfg.Limits.RequestTimeout),
		WriteTimeout:      cfg.Limits.RequestTimeout + 5*time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    cfg.Limits.MaxHeaderBytes,
	}

	listener, err := limits.Listen(cfg.NodeAddr, cfg.Limits.NodeConnections)
	if err != nil {
		return err
	}
	defer listener.Close()

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	result := make(chan error, 1)
	go func() {
		log.Printf("[Tank] storage node listening on %s", cfg.NodeAddr)
		result <- server.Serve(listener)
	}()

	select {
	case err := <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err

	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(
			context.Background(),
			10*time.Second,
		)
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
