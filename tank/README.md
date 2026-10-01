# Tank

Verifiable file storage with automatic shard repair.

Status: early development prototype.

## Features

- Four data shards and two parity shards per segment
- SHA-256 file and shard verification
- Merkle commitments
- SQLite manifests and persistent repair jobs
- Authenticated storage nodes and coordinator API
- Retrieval after one storage node fails
- Manual and automatic repair to a replacement node
- CLI with verified downloads

## Requirements

Linux or GitHub Codespaces, Go 1.26 or newer, Bash, Python 3,
curl, and OpenSSL.

## Run locally

Run these commands from the folder containing go.mod:

    go mod download
    go test -race ./...
    bash scripts/run-local.sh

Leave that terminal running. In another terminal, enter the same folder:

    set -a
    source .env.tank-local
    set +a
    go build -o bin/tank ./cmd/tank
    ./bin/tank health
    ./bin/tank list

Store a file:

    ./bin/tank tank PATH_TO_FILE

Retrieve it using the returned file ID:

    ./bin/tank retrieve FILE_ID --out NEW_OUTPUT_PATH

The output file must not already exist.

## Verification

    go test -race ./...
    go vet ./...
    go build ./...

## Current limits

Files must contain between 1 byte and 16 MiB. The coordinator buffers
whole files and uses centralized metadata. Local nodes share development
credentials. Audits download and verify shards.

Automatic repair requires enough surviving shards and an unused
configured replacement node. New files currently require all three
initial storage nodes to accept their assigned shards.

Encryption, signed receipts, blockchain registration, payments, and
production hardening are planned. Permanent retention is not guaranteed.

## License

A license has not yet been selected.
