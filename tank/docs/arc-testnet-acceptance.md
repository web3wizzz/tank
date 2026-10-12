# Arc Testnet deployment and encrypted-file acceptance

Verified on 2026-10-12. Public evidence is recorded in
[registry.json](../deploy/arc-testnet/registry.json). Mainnet, paid hosting and
real-fund spending remain unauthorized.

| Evidence | Result |
| --- | --- |
| Network | Arc Testnet, chain 5042002 |
| TankRegistry | `0xe1fC5025261af42dfe04788c7a09DAd3ea1EC435` |
| Deployment | [0x21530cecade1981109209048f67e3e72ee17d4a45dd39c65c6a031ad5072089f](https://explorer.testnet.arc.io/tx/0x21530cecade1981109209048f67e3e72ee17d4a45dd39c65c6a031ad5072089f) |
| Registration | [0xfc61320016d6fb4d54773a75f75cb773ad8998dfee60a484947dc72b796a0fab](https://explorer.testnet.arc.io/tx/0xfc61320016d6fb4d54773a75f75cb773ad8998dfee60a484947dc72b796a0fab) |
| Deployed runtime SHA-256 | `1a6840831363e03153a827765a2a6f9d16099ab0e08c94d4cbb16aa35aed7bdc` |
| Deployment fee | 0.004998504 testnet USDC |
| Registration fee | 0.001457379 testnet USDC |

## Prerequisites and custody

Official connection parameters were checked against
[Arc documentation](https://docs.arc.io/arc/references/connect-to-arc). Sandbox DNS
was restricted; outside it, DNS/TCP/TLS passed. Primary, dRPC and QuickNode public
endpoints returned HTTP403 before JSON-RPC. The documented public Blockdaemon
endpoint returned HTTP200 and chain 5042002, and supports canonical hash-pinned
receipt-block reads. No authenticated provider URL was published or transferred.

A dedicated encrypted V3 keystore and password file are kept in an owned private
directory, with file modes0600. Arc Cast's keystore omitted its optional address
field; the public sender was derived securely with `arc-cast wallet address`
and the existing password file. This custody signs locally on the worker host;
it is not a remote signing-service adapter. Arc Studio received no wallet, password,
configuration, journal, user credential, plaintext file or recovery key.

The user explicitly approved 65 Gwei deployment/registration per-gas ceilings.
All total ceilings remain unchanged: deployment 0.04 testnet USDC, each
registration 0.008, aggregate registration 0.10. Deployment reserved 0.01880424;
registration reserved 0.00548002. Both successful receipts are within their
per-gas and total caps. No other configured limit was increased.

## Journal and encrypted-file verification

Codex checked the existing signed deployment journal before any deployment
action. Resuming it returned the same address and transaction, with successful
canonical receipt and exact compiled runtime at the receipt block. Its fingerprint
remained unchanged. No second deployment or conflicting transaction was created.

A fresh synthetic file and Unicode filename were encrypted using Tank's existing
AES-GCM file envelope implementation, then uploaded through the SDK to an isolated
coordinator and four storage processes. Only ciphertext and its ID-based filename
were submitted to storage. Existing local files/accounts/data were preserved.

The separate signed worker first journaled its intent without broadcasting.
After the durable retry interval, a fresh worker process reused the identical
signed bytes, nonce and hash to submit registration. Another worker restart
completed the existing intent through canonical receipt and hash-pinned
commitment/size readback. The permanent aggregate reservation survived restart.

Independent RPC reads recomputed the manifest commitment and matched the on-chain
record at the canonical receipt block. The ciphertext size was 2133 bytes. Local
decryption matched every original byte and the original Unicode filename. A second
user's retrieval returned 404. Restarting the preserved coordinator/storage repeated
the retrieval/decryption/isolation checks without another transaction. The signing
account's registration nonce remains1, confirmed; no competing intent was added.

## Arc Studio review and repository coordination

Before delegating or transferring source, both environments were confirmed on
`main` at `67bc9322375dde280ee871d1b576d44f1553c862`. Codex's local documentation
edits were inspected and kept out of the contract-only task. Both deployment script
and test files matched by full text and SHA-256. No source transfer was necessary.

`testRejectsZeroSender` passes in both environments. The existing script already
checks chain 5042002, reads the public sender, and reverts `MissingSender` on zero
before `startBroadcast`. The reported failure was not reproduced. Arc Studio
returned an explanatory empty patch; Codex applied no artificial source change or
test weakening. Arc Studio corrected its initially unsupported tooling hypothesis.

Arc Studio's upstream Forge 1.7.1 passed all 12 existing contract tests with 256 fuzz
cases. Codex independently passed the same 12 tests on pinned Arc Forge v0.8.0-2,
including Arc/Ethereum mainnet refusal, other-network refusal, zero sender,
immutable registration, retry timestamps and registrant namespaces. Arc Studio
performed no signing, deployments, broadcasts, commits or pushes.

## Remaining hosting requirements

The commitment registry is on the public testnet. Application/storage acceptance
above uses isolated local processes; it does not establish separate-machine
hosting, public HTTPS availability, sustained operation or retention guarantees.
The pilot services were stopped after validation; their private data and worker
configuration remain available for review/restart.

Frontend/coordinator HTTPS hosts, three independent active storage machines plus
a spare, private per-node credentials, capacity/monitoring, backups/restore,
retention/funding/operator-exit terms and actual machine-failure acceptance remain
required. No paid hosting is authorized. See [pilot operations](pilot-operations.md)
and [deployment setup](arc-testnet-deployment.md).
