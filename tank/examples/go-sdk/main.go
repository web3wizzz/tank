package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"os"
	"time"

	"tank.local/tank/pkg/tank"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	token := os.Getenv("TANK_API_TOKEN")
	if token == "" {
		return fmt.Errorf("TANK_API_TOKEN is not set; source .env.tank-local first")
	}

	address := os.Getenv("TANK_API_URL")
	if address == "" {
		address = "http://127.0.0.1:8080"
	}

	client, err := tank.New(tank.Config{
		BaseURL: address,
		Token:   token,
	})
	if err != nil {
		return err
	}

	if err := client.Health(ctx); err != nil {
		return fmt.Errorf("health: %w", err)
	}
	fmt.Println("PASS: coordinator health.")

	// Fresh random data ensures each run tests a new file.
	original := make([]byte, 4096)
	if _, err := rand.Read(original); err != nil {
		return err
	}

	manifest, err := client.Tank(ctx, original)
	if err != nil {
		return fmt.Errorf("tank: %w", err)
	}
	fmt.Printf("File ID: %s\n", manifest.FileID)

	retrieved, err := client.Retrieve(ctx, manifest.FileID)
	if err != nil {
		return fmt.Errorf("retrieve: %w", err)
	}
	if !bytes.Equal(original, retrieved) {
		return fmt.Errorf("retrieved bytes differ from the original")
	}
	fmt.Println("PASS: exact verified retrieval.")

	ids, err := client.List(ctx, "")
	if err != nil {
		return fmt.Errorf("list: %w", err)
	}
	fmt.Printf("PASS: listing returned %d IDs on the first page.\n", len(ids))

	status, err := waitForRegistration(ctx, client, manifest.FileID)
	if err != nil {
		return err
	}
	fmt.Printf("PASS: automatic registration after %d attempts.\n", status.Attempts)
	if status.TransactionHash != "" {
		fmt.Printf("Transaction: %s\n", status.TransactionHash)
	}

	fmt.Println("PASS: Go SDK end-to-end demo.")
	return nil
}

func waitForRegistration(
	ctx context.Context,
	client *tank.Client,
	id string,
) (tank.Registration, error) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	previous := ""
	for {
		status, err := client.RegistrationStatus(ctx, id)
		if err != nil {
			return tank.Registration{}, fmt.Errorf("registration status: %w", err)
		}

		if status.Status != previous {
			fmt.Printf("Registration: %s\n", status.Status)
			previous = status.Status
		}

		switch status.Status {
		case "registered":
			return status, nil
		case "failed":
			return tank.Registration{}, fmt.Errorf("registration failed: %s", status.LastError)
		}

		select {
		case <-ctx.Done():
			return tank.Registration{}, fmt.Errorf("waiting for registration: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
