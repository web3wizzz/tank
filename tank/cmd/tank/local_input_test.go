package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestRegularInputsAndRecoveryBoundsRemainSupported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "recovery.json")
	want := bytes.Repeat([]byte("x"), 2048)
	if err := os.WriteFile(path, want, 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{path, alias} {
		got, err := readRecoveryFile(input)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("regular recovery read changed", err)
		}
	}
	if err := os.WriteFile(path, append(want, 'x'), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readRecoveryFile(path); err == nil {
		t.Fatal("oversized recovery accepted")
	}
	if _, err := readRecoveryFile(dir); err == nil {
		t.Fatal("directory accepted")
	}
}

func TestCLIRejectsFIFOInputsBeforeNetworking(t *testing.T) {
	// Subprocesses bound regressions in open(2) itself; an HTTP timeout cannot
	// interrupt a blocking FIFO open. No writer ever attaches to these FIFOs.
	if mode := os.Getenv("TANK_TEST_FIFO_MODE"); mode != "" {
		cmd := rootCommand()
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		args := []string{"--api", os.Getenv("TANK_TEST_FIFO_API")}
		input := os.Getenv("TANK_TEST_FIFO_PATH")
		switch mode {
		case "raw-upload":
			args = append(args, "tank", input)
		case "encrypted-upload":
			args = append(args, "tank", input, "--encrypt", "--recovery-out", input+".recovery")
		case "recovery-import":
			args = append(args, "retrieve", strings.Repeat("a", 64), "--out", input+".output", "--recovery", input)
		default:
			t.Fatal("invalid subprocess mode")
		}
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Fatal("FIFO accepted as a regular file")
		}
		return
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(503) }))
	defer server.Close()
	for _, mode := range []string{"raw-upload", "encrypted-upload", "recovery-import"} {
		t.Run(mode, func(t *testing.T) {
			for _, alias := range []bool{false, true} {
				dir := t.TempDir()
				fifo := filepath.Join(dir, "input.fifo")
				if err := syscall.Mkfifo(fifo, 0600); err != nil {
					t.Fatal(err)
				}
				input := fifo
				if alias {
					input = filepath.Join(dir, "alias")
					if err := os.Symlink(fifo, input); err != nil {
						t.Fatal(err)
					}
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCLIRejectsFIFOInputsBeforeNetworking$")
				cmd.Env = append(os.Environ(), "TANK_TEST_FIFO_MODE="+mode, "TANK_TEST_FIFO_API="+server.URL, "TANK_TEST_FIFO_PATH="+input, "TANK_API_TOKEN=synthetic-test-token")
				err := cmd.Run()
				timedOut := ctx.Err() != nil
				cancel()
				if timedOut {
					t.Fatal("CLI blocked opening a FIFO")
				}
				if err != nil {
					t.Fatal("FIFO rejection subprocess failed", err)
				}
				for _, suffix := range []string{".recovery", ".output"} {
					if _, err := os.Lstat(input + suffix); !os.IsNotExist(err) {
						t.Fatal("rejected input published output")
					}
				}
			}
		})
	}
	if requests.Load() != 0 {
		t.Fatal("rejected local input reached networking")
	}
}
