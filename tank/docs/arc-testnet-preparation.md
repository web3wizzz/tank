# Arc Testnet registry preparation

Arc is Tank's first public blockchain target, replacing Base Sepolia. Testnet
acceptance must precede any mainnet consideration. This is preparation, not a
public deployment. RPC selection, dedicated signing account and testnet USDC
funding remain pending through secure configuration. Never supply secrets in chat.

## Verified network and tooling (2026-10-11)

Sources: [connection parameters](https://docs.arc.io/arc/references/connect-to-arc),
[EVM differences](https://docs.arc.io/arc/references/evm-differences),
[gas rules](https://docs.arc.io/arc/references/gas-and-fees), and
[Arc Foundry installation](https://docs.arc.io/arc/tutorials/install-arc-foundry).
The former docs.arc.network domain redirects to docs.arc.io. Reverify before
real deployment; do not copy historical endpoint settings.

| Parameter | Arc Testnet |
| --- | --- |
| Chain ID | 5042002 |
| Native gas asset / decimals | USDC / 18 |
| Primary HTTPS / WebSocket | https://rpc.testnet.arc.io / wss://rpc.testnet.arc.io |
| Alternative HTTPS providers | https://rpc.blockdaemon.testnet.arc.io; https://rpc.drpc.testnet.arc.io; https://rpc.quicknode.testnet.arc.io |
| Alternative WebSocket providers | wss://rpc.blockdaemon.testnet.arc.io:443/websocket; wss://rpc.drpc.testnet.arc.io; wss://rpc.quicknode.testnet.arc.io |
| Explorer / faucet | https://explorer.testnet.arc.io / https://faucet.circle.com |
| Minimum fee cap | 20,000,000,000 native base units/gas (20 Gwei) |
| Documented maximum base fee / block gas | 20,000 Gwei / 30M |
| Finality | Deterministic on inclusion; one canonical confirmation |
| EVM baseline | Osaka, with selected Amsterdam features |

Endpoints above are reference information, not chosen operational defaults.
The current official connection page also lists mainnet, but Tank does not allow
its chain ID or deployment here. Tooling includes Solidity, Hardhat, ethers,
Viem/wagmi, and Arc Foundry. Use pinned Arc Foundry v0.8.0-2 (commit
`d497beea7096ff2a8e583c8b307941f24a61b06b`) for Arc simulation;
standard Anvil cannot reproduce Arc semantics. `arc-anvil --network arc` is required.
Our Solidity 0.8.28 Cancun bytecode targets a subset of Osaka, intentionally
preserving standard local Anvil support.

Tank selects EIP-1559 type-2 transactions exclusively; no blob or delegation
transaction is needed. Official docs explicitly support EIP-7702 type-4 and reject
EIP-4844 type-3 blobs. Native fee cost is `gasUsed * effectiveGasPrice / 10^18`
USDC; the upper execution bound is `gasLimit * maxFeePerGas` in native base units.
Do not apply ERC-20's 6 decimals to gas or native balances. Native and ERC-20 USDC
share one balance. Zero priority fee is accepted. Query current gas estimates and
fees; never increase configured caps silently or assume the minimum is sufficient
under load. Arc has no Base L1 fee component.

## Contract compatibility review

TankRegistry writes immutable commitment/size records under `msg.sender`, and
identical retries preserve their timestamp. It sends no native value, uses no
SELFDESTRUCT, blobs, randomness, beacon-root oracle or token balance math.
`registeredAt` is informational: sub-second Arc blocks can share timestamps.
PREVRANDAO is zero; BLOBHASH is zero and BLOBBASEFEE is one. Native transfers to
the zero address with value, burns, destructed accounts and blocklisted accounts
can revert. Native value movements emit system Transfer logs; fee deductions do
not. Tank checks its registry record and receipt, not generic Transfer logs.
Missing/failed receipts never imply success and must not cause a fresh nonce or
fee replacement: keep the signed intent reserved for review/recovery.

## Offline preparation

From `tank/`, use the Arc binaries downloaded from the official release with its
SHA-256 verified. Keep binaries outside the repository.

```bash
TANK_FORGE_BIN=/private/tools/arc-forge python3 scripts/prepare-arc-testnet.py validate-contracts
mkdir -m 700 -p data/pilot
TANK_FORGE_BIN=/private/tools/arc-forge python3 scripts/prepare-arc-testnet.py plan --out data/pilot/registry-plan-UNIQUE.json
```

The private unsigned plan includes bytecode and fingerprint, chain ID, gas bound
and missing input names. No keys, RPC credentials or account configuration enter
it. Existing plans cannot be overwritten. `deploy/arc-testnet/testnet.conf.example`
is a placeholder template: keep real configuration and signing files outside git.
The Python tool rejects signing and broadcasting. `check-rpc` uses only
`eth_chainId`, with HTTPS, redirect refusal and redacted errors. Run it only after
the selected RPC is supplied privately. No public endpoint was contacted by tests.

`contracts/script/DeployArcTestnet.s.sol` refuses other chains and zero senders.
Local tests simulate creation without any wallet/network. Real operator execution
requires separate authorization, a reviewed encrypted keystore/hardware signer,
current estimates, USDC balance, gas/fee/spend caps and exclusive nonce ownership.
Never use unlocked remote accounts, Anvil development keys, `--private-key`, shell
tracing, or printed authenticated RPC arguments. Simulate without `--broadcast`
first and retain private transaction/cache state if submission is ever authorized.

## Registration and hosting gates

The existing unlocked worker remains loopback-only on Anvil chain 31337.
Signed preparation has explicit chain/registry/registrant binding, immutable
intent journaling before submission, restart recovery, bounded fees, and
canonical hash-pinned readback. Its remote submission path remains disabled.
See [signed registration](signed-registration-preparation.md) for custody and
journal details. Local simulation cannot prove selected-provider RPC support,
public chain liveness, signer funding or a successful public deployment.

Arc hosts only the commitment registry. The frontend, coordinator and independent
storage nodes require separately selected HTTPS hosts, storage, credentials,
backups and failure-domain acceptance. A deployed registry stores no shards and
proves no continuing file availability. Follow [pilot operations](pilot-operations.md)
for three independent active storage machines plus a spare. Do not provision paid
hosts or conflate blockchain deployment with application hosting.
