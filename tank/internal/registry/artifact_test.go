package registry

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestArtifactRejectsPipeWithoutWaitingForWriter(t *testing.T) {
	if path := os.Getenv("TANK_TEST_ARTIFACT_PIPE"); path != "" {
		if _, _, err := LoadDeploymentArtifact(path); err != ErrDeployment {
			t.Fatal("pipe accepted as a compiled artifact")
		}
		return
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "artifact.fifo")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{path, alias} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestArtifactRejectsPipeWithoutWaitingForWriter$")
		cmd.Env = append(os.Environ(), "TANK_TEST_ARTIFACT_PIPE="+input)
		err := cmd.Run()
		timedOut := ctx.Err() != nil
		cancel()
		if timedOut {
			t.Fatal("compiled artifact open blocked on FIFO")
		}
		if err != nil {
			t.Fatal("artifact pipe rejection subprocess failed", err)
		}
	}
}

func TestArtifactRegularFilesAndBounds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "artifact.json")
	body := []byte(`{"bytecode":{"object":"0x010203"},"deployedBytecode":{"object":"0x040506"}}`)
	if err := os.WriteFile(path, body, 0644); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "artifact-alias")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{path, alias} {
		creation, runtime, err := LoadDeploymentArtifact(input)
		if err != nil || !bytes.Equal(creation, []byte{1, 2, 3}) || !bytes.Equal(runtime, []byte{4, 5, 6}) {
			t.Fatal("valid compiled artifact changed", err)
		}
	}
	for _, invalid := range []string{"", "null", string(body) + " {}", `{"bytecode":{"object":"0x"},"deployedBytecode":{"object":"0x01"}}`, strings.Repeat(" ", (4<<20)+1)} {
		if err := os.WriteFile(path, []byte(invalid), 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := LoadDeploymentArtifact(path); err != ErrDeployment {
			t.Fatal("invalid artifact accepted")
		}
	}
	if _, _, err := LoadDeploymentArtifact(dir); err != ErrDeployment {
		t.Fatal("artifact directory accepted")
	}
}
