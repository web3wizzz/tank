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
	"tank.local/tank/internal/node"
	"tank.local/tank/internal/storage"
)

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	backend, err := storage.NewFilesystem(cfg.NodeDataDir)
	if err != nil {
		return err
	}
	defer backend.Close()

	handler, err := node.NewHandler(backend, os.Getenv("TANK_NODE_TOKEN"))
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              cfg.NodeAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	result := make(chan error, 1)
	go func() {
		log.Printf("[Tank] storage node listening on %s", cfg.NodeAddr)
		result <- server.ListenAndServe()
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
