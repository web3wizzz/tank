<div align="center">

# ⛽ Tank

### Tank it. Retrieve it. Verify it.

**Verifiable file storage for Web3 applications.**

![Stage: Local MVP](https://img.shields.io/badge/stage-local_MVP-0F766E)
![Backend: Go](https://img.shields.io/badge/backend-Go-00ADD8?logo=go&logoColor=white)
![Contracts: Solidity](https://img.shields.io/badge/contracts-Solidity-363636?logo=solidity)
![Storage: Reed–Solomon](https://img.shields.io/badge/storage-Reed--Solomon-2563EB)

[Get started](#get-started) · [How it works](#how-it-works) · [Developer guide](#developer-guide) · [Contribute](CONTRIBUTING.md)

</div>

---

## What is Tank?

Tank stores a file across several storage nodes, adds recovery data, and checks that retrieval returns the original bytes. It can also record a cryptographic fingerprint of the file and its storage layout on a local blockchain.

For users, the workflow is simple:

**Tank a file → Keep its file ID → Retrieve it later.**

For developers, the same workflow is available through a Go CLI and an HTTP API.

**“Store Once, Retrieve Forever” is our vision.** The current release is a local development MVP. File availability depends on storage nodes and metadata remaining available; indefinite retention is not yet guaranteed.

## Why build with Tank?

| You want to… | Tank currently provides |
| --- | --- |
| Get back the exact file you stored | Hash checks on shards and the reconstructed file |
| Recover when a storage node fails | Four data shards and two recovery shards per segment |
| Restore missing storage copies | Audits, a durable repair queue, and repair to a spare node |
| Record a file commitment on-chain | A Solidity registry and background registration worker |
| Keep storage working during a chain outage | Independent storage operations and registration retries |
| Explore the complete system locally | Go services, CLI, SQLite metadata, and persistent Anvil state |

The local demo uses three initial storage nodes and a fourth spare. These run on one machine; they do not demonstrate geographic or independent-operator resilience.

## How it works

### 1. Tank it

The coordinator splits your file into segments.

Each segment becomes **4 data shards + 2 parity shards**, with two shards placed on each of three initial nodes. Any four valid shards can reconstruct that segment.

### 2. Verify it

Shard hashes and segment Merkle roots describe the stored data.

During retrieval, Tank reconstructs the file and checks its complete SHA-256 hash against the file ID.

### 3. Register it

An optional background worker records the file commitment on local Anvil.

Storage finishes independently of registration. If the chain is unavailable, the worker retries in the background.

The registry stores a commitment, file size, and registration timestamp under a registrant’s address.

**The actual file stays off-chain.** Registration is evidence of a recorded commitment, not a guarantee that storage nodes will keep the file forever.

## Get started

### Requirements

- Go: use the toolchain specified in `tank/go.mod`.
- Bash, curl, and OpenSSL for the local launcher.
- Foundry’s Anvil and Forge for optional on-chain registration.

The commands below start from the **repository root**. The Go project lives in the nested `tank/` directory.

### 1. Start local storage

```bash
cd tank
bash scripts/run-local.sh
```

Keep this terminal running.

The launcher generates `.env.tank-local` with development tokens if it does not exist, then starts four storage nodes and the coordinator.

If chain settings are already present in that file, the launcher also starts the registration worker.

### 2. Tank your first file

Open another terminal, enter the Go project directory, and run:

```bash
set -a
source .env.tank-local
set +a

go build -o bin/tank ./cmd/tank
./bin/tank health

printf 'Hello from Tank!\n' > data/local-demo/hello.txt
./bin/tank tank data/local-demo/hello.txt
```

Keep the returned file ID.

It is a **64-character SHA-256 hash**, without a `0x` prefix in CLI and HTTP requests.

### 3. Retrieve and compare

Replace the value below with your returned file ID:

```bash
FILE_ID='paste-your-file-id-here'

./bin/tank retrieve "$FILE_ID" --out data/local-demo/hello-retrieved.txt

cmp data/local-demo/hello.txt data/local-demo/hello-retrieved.txt
```

No output from `cmp` means the files match.

The CLI does not overwrite an existing destination, so choose a new output filename for repeated downloads.

### Optional: enable local blockchain registration

In a separate terminal, from the Go project directory:

```bash
bash scripts/run-anvil.sh
```

The script uses `$HOME/.foundry/bin/anvil` by default. Set `TANK_ANVIL_BIN` to use another installed executable.

It loads and saves `data/local-demo/anvil-state.json`, with periodic snapshots every 10 seconds.

Use Ctrl+C for a clean shutdown. Abrupt termination can lose changes since the last snapshot.

On a fresh chain, deploy the registry from another terminal:

```bash
cd contracts

"$HOME/.foundry/bin/forge" create src/TankRegistry.sol:TankRegistry --rpc-url http://127.0.0.1:8545 --unlocked --from 0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266 --broadcast
```

Add these settings to the existing `tank/.env.tank-local`, keeping its generated tokens.

Replace the contract address with the actual **Deployed to** address:

```dotenv
TANK_CHAIN_RPC=http://127.0.0.1:8545
TANK_CHAIN_ID=31337
TANK_REGISTRY_ADDR=YOUR_DEPLOYED_CONTRACT_ADDRESS
TANK_REGISTRANT=0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266
```

Restart `scripts/run-local.sh` so it reads the new settings.

The worker discovers existing manifests as well as newly tanked files.

The current registration client is restricted to local Anvil, chain ID **31337**, and an unlocked development account. Public testnet deployment and production signing are planned.

## Developer guide

### HTTP API

Default coordinator: `http://127.0.0.1:8080`.

All routes below except `/health` require:

```http
Authorization: Bearer <TANK_API_TOKEN>
```

| Method | Route | Purpose |
| --- | --- | --- |
| `GET` | `/health` | Check coordinator health |
| `POST` | `/tank` | Store a raw binary request body and return its manifest |
| `GET` | `/retrieve/{file_id}` | Reconstruct, verify, and return file bytes |
| `GET` | `/list?after={file_id}` | List file IDs with cursor pagination |
| `POST` | `/repair/{file_id}` | Repair missing shards to a configured replacement node |
| `GET` | `/registrations/{file_id}` | Read registration job status when chain integration is configured |

Tank a file from the Go project directory:

```bash
curl --fail --silent --show-error -H "Authorization: Bearer $TANK_API_TOKEN" -H "Content-Type: application/octet-stream" --data-binary @data/local-demo/hello.txt http://127.0.0.1:8080/tank
```

Check its registration:

```bash
curl --fail --silent --show-error -H "Authorization: Bearer $TANK_API_TOKEN" "http://127.0.0.1:8080/registrations/$FILE_ID"
```

Registration states:

| State | Meaning |
| --- | --- |
| `not_queued` | No registration job exists for this file under the configured target |
| `pending` | Waiting to be processed or retried |
| `submitted` | A transaction hash has been saved |
| `registered` | The worker verified the matching on-chain record |
| `failed` | Processing encountered a terminal failure |

Successful storage does not mean registration has completed.

A `registered` status records a successful worker check; it does not continuously revalidate the chain. If Anvil state is deleted or reset, local job statuses can become stale.

### Storage and commitment details

| Setting | Current implementation |
| --- | --- |
| Maximum file size | 16 MiB |
| Local launcher segment size | 4 MiB |
| Encoding | Reed–Solomon: 4 data + 2 parity shards per segment |
| Verification | SHA-256 hashes and Merkle roots |
| Metadata | SQLite manifests and persistent job queues |
| Initial placement | Three nodes |
| Replacement capacity | One spare node in the local demo |
| Blockchain | Local Anvil, chain ID 31337 |

The file commitment binds the file ID, size, encoding parameters, and ordered segment roots.

Node locations are excluded from the commitment, so repair can relocate shards without changing it.

The contract accepts:

```solidity
tank(bytes32 fileId, bytes32 commitment, uint64 fileSize)
```

And exposes:

```solidity
getTank(address registrant, bytes32 fileId)
```

Records are namespaced by registrant.

Identical registration retries preserve the original timestamp. Conflicting commitments or sizes are rejected.

The local coordinator uses one configured registrant account. That account is not a separate ownership identity for each API user.

### Project layout

Paths are relative to the repository root:

| Path | Responsibility |
| --- | --- |
| `tank/cmd/` | Coordinator, storage node, CLI, and registration executables |
| `tank/internal/coordinator/` | Storage orchestration, retrieval, auditing, repair, and HTTP API |
| `tank/internal/encoding/` | Reed–Solomon encoding and reconstruction |
| `tank/internal/integrity/` | Hashing and Merkle verification |
| `tank/internal/metadata/` | SQLite manifests, migrations, and durable queues |
| `tank/internal/node/` | Storage node HTTP server and client |
| `tank/internal/storage/` | Filesystem storage |
| `tank/internal/registry/` | Ethereum client and registration worker |
| `tank/contracts/` | Solidity registry and Foundry tests |
| `tank/scripts/` | Local storage and persistent Anvil launchers |
| `tank/docs/` | Additional guides |

### Run checks

From the Go project directory:

```bash
go test -race ./...
go build ./cmd/...
```

From `tank/contracts/`:

```bash
"$HOME/.foundry/bin/forge" test
```

## Progress and next steps

| Working and verified locally | Planned |
| --- | --- |
| Authenticated storage API and Go CLI | Go and TypeScript SDKs |
| File recovery after node failure | Browser interface |
| Manual and queued background repair | Independent operator deployments |
| Automatic registration and RPC outage recovery | Public testnet registration and secure signing |
| Exact retrieval after registration | Larger-file streaming and performance measurements |
| On-chain record preserved across Anvil restart | Chain reset detection and status revalidation |
| Go race tests and GitHub CI | Retention economics, storage proofs, and operator incentives |

Current audits read shards to check availability and integrity.

Data availability sampling, proof-of-retrievability, staking, and slashing are research directions, not implemented guarantees.

## Contribute

Help make verifiable storage easier to use.

Documentation fixes, reproducible bug reports, meaningful tests, and focused code improvements are welcome.

Read [CONTRIBUTING.md](CONTRIBUTING.md) for setup, checks, and pull request guidance.

Open an issue before starting a large architecture change.

Useful starting points include clearer setup errors, registration status tests, and a repeatable end-to-end demo.

## Support the project

If Tank is useful to you:

- Star the repository.
- Share a reproducible demo.
- Report bugs with clear reproduction steps.
- Help improve documentation or resolve an issue.

For sponsorship or collaboration, use the maintainer contact details on the repository owner’s GitHub profile.

## Current boundaries

Tank is a development MVP.

Its local nodes share one machine, metadata depends on SQLite, and no file encryption or multi-user authorization model has been implemented.

Development tokens and Anvil’s public accounts are for local testing.

Do not commit `.env.tank-local`, chain snapshots, databases, or stored file data.

S3 describes the product vision; an S3-compatible API is not implemented.

Erasure coding supports recovery within its threshold, but it does not create a perpetual storage guarantee.

## License

Check the repository’s license files before reuse.

This README does not grant a license. An explicit project-wide license needs to be established if one has not already been added.

---

<div align="center">

**Tank it. Retrieve it. Verify it.**

Building toward verifiable storage for the decentralized internet.

</div>
