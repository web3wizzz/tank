# ⛽ TANK

> **S3 for Web3.**
> **Store Once, Retrieve Forever.**

TANK is a decentralized storage system designed for the Web3 ecosystem.

Instead of simply uploading a file to a centralized bucket, you **tank it**.

TANK splits your file into redundant shards, distributes those shards across storage nodes, creates a cryptographic Merkle tree for verification, and registers the file's proof on-chain.

The result is a storage system designed around three principles:

**Redundancy. Verification. Ownership.**

---

## What is TANK?

TANK provides an S3-like experience for Web3 developers while exposing decentralized storage primitives through an API, CLI, and SDKs.

The core workflow is simple:

```text
Tank → Store a file
Retrieve → Get the file back
Proof → Verify its integrity
```

Instead of:

```text
upload()
download()
```

TANK uses:

```go
tank()
retrieve()
```

Because in TANK:

> **You don't upload files. You tank them.**

---

# ⚡ How It Works

When a file is tanked:

```text
                    ┌──────────────┐
                    │     File     │
                    └──────┬───────┘
                           │
                           ▼
                 ┌──────────────────┐
                 │ Reed-Solomon     │
                 │ Erasure Coding   │
                 └────────┬─────────┘
                          │
                 150 total shards
                          │
            ┌─────────────┼─────────────┐
            ▼             ▼             ▼
        Node 1         Node 2         Node 3
        shards         shards         shards
            │             │             │
            └─────────────┼─────────────┘
                          │
                          ▼
                  ┌──────────────┐
                  │ Merkle Tree  │
                  └──────┬───────┘
                         │
                         ▼
                 Merkle Root / Proof
                         │
                         ▼
                  Base Sepolia
                  TankRegistry
```

A file is encoded into:

**100 data shards + 50 parity shards = 150 total shards.**

This means the original file can be reconstructed as long as enough shards remain available.

---

# 🧬 Core Architecture

TANK Phase 1 consists of five major components:

```text
tank/
├── contracts/    → On-chain registry
├── coordinator/  → Go storage coordinator
├── cli/          → tank command-line client
├── sdk-ts/       → TypeScript SDK
├── sdk-go/       → Go SDK
└── frontend/     → Next.js interface
```

### Coordinator

The Go coordinator is the heart of the system.

It handles:

* file ingestion
* Reed-Solomon encoding
* chunk distribution
* Merkle tree generation
* local storage
* file reconstruction
* proof generation
* blockchain registration
* retrieval

### Smart Contract

`TankRegistry.sol` records the cryptographic identity of tanked files on-chain.

```solidity
tank(fileId, merkleRoot)
```

The contract stores:

```text
File ID
Merkle Root
Timestamp
Owner
```

The blockchain therefore acts as the **public proof layer**, not the file-storage layer.

### CLI

The `tank` CLI provides a developer-friendly interface:

```bash
tank tank ./file.pdf
tank retrieve 0x...
tank proof 0x... 12
tank list
tank health
```

### SDKs

Developers can integrate TANK directly into their applications.

TypeScript:

```ts
const tank = new Tank();

const result = await tank.tank(file);

await tank.retrieve(result.fileId);
```

Go:

```go
client := tank.New("http://localhost:8080")

fileID, merkleRoot, err := client.Tank(data)
```

---

# 🔐 Cryptographic Verification

TANK doesn't ask users to blindly trust the storage layer.

Every chunk is hashed with SHA-256.

The hashes are then assembled into a binary Merkle tree.

The resulting Merkle root becomes the cryptographic fingerprint of the complete shard set.

```text
Chunk 0 ──┐
Chunk 1 ──┘
             ┐
Chunk 2 ──┐  ├── Hash
Chunk 3 ──┘  ┘
               │
               ▼
          Merkle Root
               │
               ▼
          Blockchain
```

A proof request can return:

* chunk data
* Merkle proof
* Merkle root

This allows the integrity of an individual chunk to be independently verified.

---

# 🧩 Reed-Solomon Redundancy

TANK uses Reed-Solomon erasure coding:

```text
100 data shards
+
50 parity shards
=
150 total shards
```

The parity shards provide redundancy without requiring every storage node to maintain a complete copy of the original file.

For the Phase 1 local implementation, TANK simulates three storage nodes:

```text
./data/node1
./data/node2
./data/node3
```

Chunks are distributed across the nodes in round-robin order.

During retrieval, TANK collects enough available shards to reconstruct the original file.

---

# ⛓️ On-Chain Proof

The Phase 1 implementation uses **Base Sepolia** for blockchain registration.

The `TankRegistry` contract provides:

```solidity
function tank(
    bytes32 fileId,
    bytes32 merkleRoot
) external;
```

And allows the proof to be retrieved with:

```solidity
function getTank(
    bytes32 fileId
) external view returns (bytes32);
```

A successful transaction creates a permanent public record that associates:

```text
fileId
   ↓
merkleRoot
   ↓
owner
   ↓
timestamp
```

The actual file remains off-chain.

---

# 🚀 API

The coordinator exposes a simple REST API.

### Tank a file

```http
POST /tank
```

Uploads a file, encodes it, stores its chunks, generates the Merkle root, and registers the proof on-chain.

Example response:

```json
{
  "fileId": "0x...",
  "merkleRoot": "0x...",
  "chunks": 150
}
```

### Retrieve a file

```http
GET /retrieve/:fileId
```

Reconstructs and returns the original file.

### Retrieve a proof

```http
GET /proof/:fileId/:index
```

Returns the selected chunk and its Merkle proof.

### List files

```http
GET /list
```

### Health

```http
GET /health
```

Response:

```json
{
  "status": "ok"
}
```

---

# 🖥️ CLI

TANK provides a first-class developer CLI.

### Tank a file

```bash
tank tank ./document.pdf
```

Example:

```text
✓ Tanked!

FileId: 0x8a...
Merkle: 0x72...
Chunks: 150

Link:
http://localhost:3000/retrieve/0x8a...
```

### Retrieve

```bash
tank retrieve 0x8a... --out ./document.pdf
```

### Verify a chunk proof

```bash
tank proof 0x8a... 12
```

### List tanked files

```bash
tank list
```

### Check coordinator

```bash
tank health
```

---

# 📦 TypeScript SDK

Package:

```text
@tank-storage/sdk
```

Example:

```ts
import { Tank } from "@tank-storage/sdk";

const tank = new Tank("http://localhost:8080");

const result = await tank.tank(file);

console.log(result.fileId);
console.log(result.merkleRoot);

const restored = await tank.retrieve(result.fileId);
```

Available methods:

```ts
tank()
retrieve()
getProof()
list()
```

---

# 🐹 Go SDK

The Go SDK provides the same functionality for Go applications.

```go
client := tank.New("http://localhost:8080")

fileID, merkleRoot, err := client.Tank(data)
if err != nil {
    log.Fatal(err)
}

file, err := client.Retrieve(fileID)
if err != nil {
    log.Fatal(err)
}
```

---

# 🏗️ Technology Stack

### Coordinator

* Go 1.22+
* Fiber v2
* `klauspost/reedsolomon`
* Viper
* `go-ethereum`

### Smart Contracts

* Solidity `^0.8.20`
* Foundry
* Base Sepolia

### Frontend

* Next.js 14
* React
* TypeScript

### SDKs

* TypeScript
* Go

### Cryptography

* SHA-256
* Merkle trees
* Reed-Solomon erasure coding

---

# 📁 Project Structure

```text
tank/
│
├── contracts/
│   ├── TankRegistry.sol
│   └── deploy.sh
│
├── coordinator/
│   ├── main.go
│   ├── chunker.go
│   ├── merkle.go
│   ├── store.go
│   ├── config.go
│   └── go.mod
│
├── cli/
│   ├── main.go
│   └── go.mod
│
├── sdk-ts/
│   ├── src/
│   │   └── index.ts
│   ├── package.json
│   └── tsconfig.json
│
├── sdk-go/
│   ├── client.go
│   └── go.mod
│
├── frontend/
│   ├── app/
│   │   ├── page.tsx
│   │   ├── layout.tsx
│   │   └── globals.css
│   ├── package.json
│   └── .env.example
│
└── README.md
```

---

# 🧪 Phase 1 Goals

The goal of Phase 1 is not to create a massive decentralized cloud.

The goal is to prove the complete architecture locally and end-to-end.

A successful Phase 1 flow is:

```text
File
 ↓
tank()
 ↓
Chunk into 150 shards
 ↓
Distribute across simulated nodes
 ↓
Generate Merkle root
 ↓
Register proof on Base Sepolia
 ↓
Return file ID
 ↓
retrieve()
 ↓
Collect available shards
 ↓
Reconstruct original file
 ↓
Return original bytes
```

---

# 🎯 Design Principles

### 1. Simple Developer Experience

TANK should feel as simple as object storage.

```text
tank()
retrieve()
```

The underlying cryptography and distributed-storage mechanics should remain abstracted from the developer.

### 2. Verifiable by Design

Every tanked file should have a cryptographic identity that can be independently verified.

### 3. Redundancy Over Duplication

Use erasure coding to create resilience without simply copying the entire file everywhere.

### 4. Blockchain as the Proof Layer

Use the blockchain for:

* ownership
* timestamps
* Merkle roots
* public verification

Do not put raw file data on-chain.

### 5. Modular Architecture

The simulated storage layer in Phase 1 should be replaceable with real storage nodes in future phases without redesigning the entire system.

---

# 🗺️ Roadmap

## Phase 1 — Local MVP

* [x] Architecture
* [ ] File chunking
* [ ] Reed-Solomon encoding
* [ ] Three-node local storage simulation
* [ ] Merkle tree generation
* [ ] File reconstruction
* [ ] REST coordinator
* [ ] TankRegistry contract
* [ ] Base Sepolia deployment
* [ ] CLI
* [ ] TypeScript SDK
* [ ] Go SDK
* [ ] Next.js frontend

## Phase 2 — Real Storage Nodes

Replace local simulated nodes with independently running storage nodes.

```text
Coordinator
    ↓
Node 1
Node 2
Node 3
Node 4
...
```

Nodes should be independently addressable and replaceable.

## Phase 3 — Permissionless Storage

Introduce mechanisms for third-party node operators to participate in the network.

Potential future features:

* node registration
* node health
* storage capacity
* availability monitoring
* proof-of-storage
* node reputation
* cryptographic node identity

## Phase 4 — Production Network

Expand toward a real decentralized storage network with:

* multiple storage providers
* geographically distributed nodes
* stronger redundancy
* automated repair
* persistent storage
* SDK integrations
* production-grade monitoring

---

# 🔒 Security

TANK handles user data and blockchain transactions, so security is a core requirement.

Never commit:

```text
TANK_PRIVATE_KEY
PRIVATE_KEY
RPC credentials
API secrets
wallet seed phrases
```

Use environment variables.

Do not store user private keys.

Do not put private file contents on-chain.

Validate all uploaded files.

Verify blockchain transactions server-side.

Treat the smart contract as security-critical infrastructure.

---

# 🌐 The Vision

TANK starts with a simple question:

> **What would S3 look like if files were designed for the decentralized internet from the beginning?**

The answer is TANK.

Not another file uploader.

Not another blockchain wrapper.

A storage primitive designed for Web3 applications.

Developers should be able to build:

```text
NFT platforms
AI datasets
DePIN applications
Decentralized social networks
Gaming infrastructure
DAOs
On-chain applications
Web3 SaaS
```

without having to reinvent distributed file storage.

---

# ⛽ TANK

### Store Once. Retrieve Forever.

**Tank it. Retrieve it. Verify it.**

Built for the decentralized internet.
