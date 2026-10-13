# Automatic registration

Tank stores files independently of chain availability.

The registration worker discovers saved manifests and queues their
commitments for the configured registry and registrant.

Run the worker from the Go project directory:

    set -a
    source .env.tank-local
    set +a
    ./bin/tank-registration-worker

The current worker supports local Anvil on chain ID 31337 and uses
an unlocked development account.

Authenticated status endpoint:

    GET /registrations/{file_id}

States: not_queued, pending, submitted, registered, failed.

RPC failures retry with increasing delays. Saved transaction hashes
allow processing to resume after a worker restart.

Verified locally:
- Automatic registration of a new file.
- Recovery after an RPC outage.
- Exact retrieval of the registered file.

Anvil state resets can invalidate earlier registration statuses.
Persistent chain state and revalidation remain follow-up work.

## Arc preparation

**Current status (2026-10-13):** the existing TankRegistry and signed Arc
registration passed public testnet verification. See
[Arc acceptance](arc-testnet-acceptance.md). The public worker uses guarded
`tank-prepare-registration --broadcast` with the shared durable journal; the
coordinator uses `arc-testnet-external` status mode. The old local worker below
remains restricted to loopback Anvil.

Arc Testnet (chain 5042002) is the first public registry target. The unlocked
worker above stays restricted to loopback Anvil chain31337. Public state is
namespaced by chain, registry address and registrant; a shared signer has one
exclusive nonce lane across registry contracts.

The [signed registration worker](signed-registration-preparation.md) signs and
journals through encrypted private custody, checks native USDC fee bounds, and
reconciles canonical readback. Its explicit `--broadcast` mode is Arc Testnet
only. A disabled-by-default systemd timer template runs bounded passes; see
[pilot operations](pilot-operations.md). The registry stores commitments only;
frontend, coordinator, and independent storage-node hosting remain separate pilot
work.
