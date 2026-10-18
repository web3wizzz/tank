package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"tank.local/tank/internal/metadata"
	"time"
)

type destinations []string

func (d *destinations) String() string         { return "explicit private checkpoint directories" }
func (d *destinations) Set(value string) error { *d = append(*d, value); return nil }

func run(args []string, output io.Writer) error {
	flags := flag.NewFlagSet("tank-backup", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var replicas destinations
	flags.Var(&replicas, "replica", "new private checkpoint directory; repeat at least twice")
	verify := flags.String("verify", "", "verify an existing checkpoint without activating it")
	expected := flags.String("expected-sha256", "", "separately pinned checkpoint digest for verification")
	source := flags.String("db", os.Getenv("TANK_DATABASE_PATH"), "existing metadata database")
	destination := flags.String("out", "", "new private snapshot destination")
	timeout := flags.Duration("timeout", 2*time.Minute, "bounded backup deadline")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("invalid backup arguments; use --db PATH with --out PATH or repeated --replica PATH; use --verify PATH to check a bundle")
	}
	if flags.NArg() != 0 || *timeout < time.Second || *timeout > time.Hour {
		return fmt.Errorf("unexpected arguments or invalid timeout; timeout must be 1s–1h")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if *verify != "" {
		if *destination != "" || len(replicas) != 0 {
			return fmt.Errorf("verification cannot create snapshots")
		}
		if _, err := metadata.VerifyCheckpoint(ctx, *verify, *expected); err != nil {
			return err
		}
		_, err := fmt.Fprintln(output, "Checkpoint byte integrity verified; freshness and recovery activation are not established.")
		return err
	}
	if *expected != "" || *source == "" {
		return fmt.Errorf("source database required; expected digest applies only to verification")
	}
	if len(replicas) != 0 {
		if *destination != "" {
			return fmt.Errorf("choose --out or repeated --replica options")
		}
		completed, err := metadata.CreateCheckpoints(ctx, *source, replicas)
		if err != nil {
			return fmt.Errorf("replicated checkpoint failed after %d verified copies; partial and published destinations retained: %w", len(completed), err)
		}
		_, err = fmt.Fprintln(output, "Identical private checkpoint copies verified. Operator must establish independent custody and newest authoritative state; signing activation remains gated.")
		return err
	}
	if *destination == "" {
		return fmt.Errorf("use --out NEW_SNAPSHOT or at least two --replica NEW_DIRECTORY options")
	}
	if err := metadata.BackupDatabase(ctx, *source, *destination); err != nil {
		return err
	}
	_, err := fmt.Fprintln(output, "Consistent private metadata snapshot created. Keep node storage and browser recovery keys backed up separately.")
	return err
}
func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
