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
- CLI and Go/TypeScript SDKs with verified downloads
- Per-user credentials, ownership-scoped lists, and revocable sessions
- Browser encryption and recovery with the original filename
- Optional local on-chain registration with background retries

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

For the packaged local workspace, see [Docker/Compose instructions](docs/local-containers.md).
This uses isolated persistent volumes and does not enable Arc signing.

    go test -race ./...
    go vet ./...
    go build ./...

## Current limits

Files must contain between 1 byte and 16 MiB. The coordinator buffers
whole files and uses centralized metadata. Local nodes share development
credentials. Audits download and verify shards.

Automatic repair requires enough surviving shards and an unused
configured replacement node in a safe failure domain. New files select three
eligible healthy domains, including spares, with whole-file capacity checks;
see [placement configuration and limitations](docs/storage-placement.md).

Browser encryption, individual user authorization, and local blockchain registration
are implemented. Signed Arc Testnet deployment and registration are verified in
[Arc acceptance](docs/arc-testnet-acceptance.md). CLI/SDK uploads remain unencrypted
by default; [compatible opt-in helpers and CLI flags](docs/sdk-encryption.md)
provide recovery-key export/import. Payments and production durability remain planned; permanent retention
is not guaranteed. The [independent-host pilot handoff](docs/independent-host-pilot-handoff.md)
documents hosting inputs and acceptance gates; no hosted storage pilot is yet verified.

See the [repository README](../README.md) for browser setup and
[local MVP guide](docs/local-mvp.md) for isolated integration checks.

## License

A license has not yet been selected.
