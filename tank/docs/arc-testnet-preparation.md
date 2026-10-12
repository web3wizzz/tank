# Arc Testnet registry preparation

Arc is Tank's first public blockchain target, replacing Base Sepolia. Testnet
acceptance must precede any mainnet consideration. Guarded deployment and explicit
worker broadcasting are now implemented; see [deployment setup](arc-testnet-deployment.md).
RPC connectivity, dedicated signing account and testnet USDC funding remain pending
through secure configuration. Never supply secrets in chat.

## Verified network and tooling (2026-10-12)

Sources: [connection parameters](https://docs.arc.io/arc/references/connect-to-arc),
[EVM differences](https://docs.arc.io/build/evm-differences),
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
The official [release](https://github.com/circlefin/arc-foundry/releases/tag/v0.8.0-2)
is pinned by commit and archive SHA-256 in the installer.
Our Solidity 0.8.28 Cancun bytecode targets a subset of Osaka, intentionally
preserving standard local Anvil support.

Tank selects EIP-1559 type-2 transactions exclusively; no blob or delegation
transaction is needed. Isolated Arc Anvil tests accept legacy type-0, access-list
type-1, type-2 and set-code type-4 transactions; this is local tooling evidence,
not selected-provider acceptance. Official docs explicitly support EIP-7702 type-4 and reject
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
Local tests simulate creation without any wallet/network. The user has authorized
testnet-only execution once prerequisites pass. Use the guarded CLI in the
deployment guide, with a dedicated encrypted keystore,
current estimates, USDC balance, gas/fee/spend caps and exclusive nonce ownership.
Never use unlocked remote accounts, Anvil development keys, `--private-key`, shell
tracing, or printed authenticated RPC arguments. Simulate without `--broadcast`
first and retain private transaction/cache state if submission is ever authorized.

## Local Arc acceptance evidence

From `tank/` on Linux x86_64:

```bash
bash scripts/install-arc-foundry.sh /tmp/tank-arc-tools-NEW
TANK_ARC_FORGE_BIN=/tmp/tank-arc-tools-NEW/arc-forge TANK_ARC_ANVIL_BIN=/tmp/tank-arc-tools-NEW/arc-anvil bash scripts/test-arc-local.sh
```

The installer requires a new directory and verifies the release digest before
extracting binaries. Contract CI runs both standard Foundry and the pinned Arc
suite. Tests spawn owned loopback nodes with no unlocked development accounts,
public RPC, forks, real wallets or production databases. Generated test-only
accounts sign locally; `anvil_setBalance` provides synthetic local funds.

Evidence covers registry creation, type-2 signed registration, native USDC
receipt fee versus balance delta, canonical hash-pinned readback, absent-receipt
retry, immutable-byte retry, and SQLite reopen after submission before completion.
Recovery confirms the journaled intent without invoking the signer again.
Contract tests separately check identical registration preserves its timestamp
and conflicting data reverts. Local transaction checks also verify system native
Transfer logs and rejection of nonzero transfers to the zero address.

Arc Anvil v0.8.0-2's gas estimator accepted that forbidden zero-address transfer,
but execution rejected it. Estimates are not success evidence; Tank requires a
successful canonical receipt plus matching registry readback. The official gas
page's native-send example uses 6-decimal conversion despite its native precision
table specifying 18 decimals. Tank follows the native precision table and EVM
interface documentation, confirmed by the local balance/fee tests; it sends zero
native value for registration.

## Registration and hosting gates

The existing unlocked worker remains loopback-only on Anvil chain 31337.
Signed preparation has explicit chain/registry/registrant binding, immutable
intent journaling before submission, restart recovery, bounded fees, and
canonical hash-pinned readback. Its default preparation mode never submits.
Explicit authorized mode uses the separate bounded immutable-byte submitter and
durable aggregate budget.
See [signed registration](signed-registration-preparation.md) for custody and
journal details. Local simulation cannot prove selected-provider RPC support,
public chain liveness, signer funding or a successful public deployment.

Arc hosts only the commitment registry. The frontend, coordinator and independent
storage nodes require separately selected HTTPS hosts, storage, credentials,
backups and failure-domain acceptance. A deployed registry stores no shards and
proves no continuing file availability. Follow [pilot operations](pilot-operations.md)
for three independent active storage machines plus a spare. Do not provision paid
hosts or conflate blockchain deployment with application hosting.
