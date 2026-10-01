package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"
)

func rootCommand() *cobra.Command {
	var address string
	var timeout time.Duration
	var client *apiClient

	root := &cobra.Command{
		Use:           "tank",
		Short:         "Tank files and retrieve verified bytes",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			var err error
			client, err = newAPI(address, timeout)
			return err
		},
	}

	root.PersistentFlags().StringVar(
		&address, "api", "http://127.0.0.1:8080", "coordinator URL",
	)
	root.PersistentFlags().DurationVar(
		&timeout, "timeout", 2*time.Minute, "HTTP request timeout",
	)

	root.AddCommand(&cobra.Command{
		Use:   "health",
		Short: "Check the coordinator",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := client.request(
				cmd.Context(), http.MethodGet, "/health", nil, false,
			)
			if err != nil {
				return err
			}
			defer res.Body.Close()

			fmt.Fprintln(cmd.OutOrStdout(), "Tank coordinator is healthy.")
			return nil
		},
	})

	root.AddCommand(&cobra.Command{
		Use:   "tank FILE",
		Short: "Store a file across the storage nodes",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			file, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer file.Close()

			info, err := file.Stat()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() ||
				info.Size() < 1 || info.Size() > maxFileBytes {
				return fmt.Errorf("input must be a regular file containing 1 byte to 16 MiB")
			}

			hash := sha256.New()
			body := io.TeeReader(io.LimitReader(file, maxFileBytes+1), hash)

			res, err := client.request(
				cmd.Context(), http.MethodPost, "/tank", body, true,
			)
			if err != nil {
				return err
			}
			defer res.Body.Close()

			var result struct {
				FileID string `json:"file_id"`
			}
			if err := decodeJSON(res.Body, &result); err != nil {
				return err
			}

			if !validFileID(result.FileID) ||
				result.FileID != hex.EncodeToString(hash.Sum(nil)) {
				return fmt.Errorf("server file ID does not match the sent bytes")
			}

			// Print only the ID so shell scripts can capture it.
			fmt.Fprintln(cmd.OutOrStdout(), result.FileID)
			return nil
		},
	})

	var output string
	retrieve := &cobra.Command{
		Use:   "retrieve FILE_ID --out PATH",
		Short: "Retrieve a file and verify its content hash",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if !validFileID(id) {
				return fmt.Errorf("invalid file ID")
			}
			if output == "" {
				return fmt.Errorf("--out is required")
			}

			res, err := client.request(
				cmd.Context(), http.MethodGet, "/retrieve/"+id, nil, true,
			)
			if err != nil {
				return err
			}
			defer res.Body.Close()

			count, err := saveVerified(res.Body, id, output)
			if err != nil {
				return err
			}

			fmt.Fprintf(
				cmd.OutOrStdout(),
				"Verified %d bytes. Saved to %s\n",
				count, output,
			)
			return nil
		},
	}
	retrieve.Flags().StringVar(&output, "out", "", "new output file path")
	root.AddCommand(retrieve)

	var after string
	list := &cobra.Command{
		Use:   "list",
		Short: "List up to 100 file IDs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if after != "" && !validFileID(after) {
				return fmt.Errorf("invalid --after file ID")
			}

			path := "/list"
			if after != "" {
				path += "?after=" + url.QueryEscape(after)
			}

			res, err := client.request(
				cmd.Context(), http.MethodGet, path, nil, true,
			)
			if err != nil {
				return err
			}
			defer res.Body.Close()

			var ids []string
			if err := decodeJSON(res.Body, &ids); err != nil {
				return err
			}

			for _, id := range ids {
				fmt.Fprintln(cmd.OutOrStdout(), id)
			}
			return nil
		},
	}
	list.Flags().StringVar(&after, "after", "", "last file ID from the previous page")
	root.AddCommand(list)

	return root
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := rootCommand().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "[Tank]", err)
		os.Exit(1)
	}
}
