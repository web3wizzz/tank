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

func run(args []string, output io.Writer) error {
	flags := flag.NewFlagSet("tank-backup", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	source := flags.String("db", os.Getenv("TANK_DATABASE_PATH"), "existing metadata database")
	destination := flags.String("out", "", "new private snapshot destination")
	timeout := flags.Duration("timeout", 2*time.Minute, "bounded backup deadline")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("invalid backup arguments; use --db PATH --out NEW_PATH [--timeout 2m]")
	}
	if flags.NArg() != 0 || *source == "" || *destination == "" || *timeout < time.Second || *timeout > time.Hour {
		return fmt.Errorf("use --db EXISTING_DATABASE --out NEW_SNAPSHOT; timeout must be 1s–1h")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
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
