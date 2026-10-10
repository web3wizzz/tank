# Base Sepolia registry preparation

This repository prepares a reproducible contract artifact and a guarded Foundry
script. No public RPC, signer, infrastructure, or testnet transaction was selected
or used for the preparation tests. Actual deployment/broadcast remains blocked
until the operator provides inputs and authorization.

Base Sepolia uses chain ID **84532**, distinct from local Anvil **31337** and Base
mainnet. See [official network details](https://docs.base.org/get-started/connect-to-base).
No RPC default is filled in: choose the approved provider privately later.

## Offline preparation

From `tank/` with pinned Foundry installed:

```bash
python3 scripts/prepare-base-sepolia.py validate-contracts
mkdir -m 700 -p data/pilot
python3 scripts/prepare-base-sepolia.py plan --out data/pilot/registry-plan-UNIQUE.json
```

Validation builds/tests in the isolated Forge VM. The unsigned private plan
contains creation bytecode, its SHA-256 build fingerprint, network/chain ID,
a bounded gas ceiling, and the names of missing configuration inputs. It does
not contain an RPC URL, wallet account name, credentials, keys, or signatures.
Existing plans are never overwritten. `TANK_BASE_SEPOLIA_MAX_GAS` defaults to
1000000 and accepts 21000–10000000; measure real deployment estimates before using
that ceiling. A plan is an artifact, not a signed transaction or a fee guarantee.

`deploy/base-sepolia/testnet.conf.example` lists placeholders for a selected HTTPS
RPC, public sender address, named encrypted keystore account, gas ceiling, and
confirmation policy. Keep its real configuration outside the repository, private,
and never paste private keys into the template or command line.

`contracts/script/DeployBaseSepolia.s.sol` rejects every chain except84532 and a
zero sender. It reads the public sender from `TANK_BASE_SEPOLIA_SENDER`. Foundry
signing uses the future selected encrypted wallet; the script contains no private
key handling. Tests simulate creation in a local EVM and exercise both guards.
These tests are not actual Base Sepolia deployment evidence.

## Read-only preflight once inputs exist

Set `TANK_BASE_SEPOLIA_RPC` privately to the selected HTTPS endpoint, then run:

```bash
python3 scripts/prepare-base-sepolia.py check-rpc
```

The only RPC method is `eth_chainId`. Wrong-chain responses, redirects, oversized
responses, and provider errors fail with redacted messages. No transaction or
wallet access occurs. This step was tested with local mocks; no real endpoint is
chosen by the preparation scripts.

## Future operator deployment procedure — not executed

After explicit deployment authorization and selection of the sender/wallet:

1. Review the source commit, contract tests, unsigned bytecode plan, testnet chain,
   sender balance/nonce, provider limits, gas/fee ceilings, and signer custody.
   Use an encrypted keystore or reviewed hardware signer; never the public Anvil
   development key. Fees and funding remain operator decisions.
2. Export the chosen RPC through Foundry's private `ETH_RPC_URL` environment and
   the public sender through `TANK_BASE_SEPOLIA_SENDER`. Do not pass authenticated
   RPC secrets or private keys as printed command arguments.
3. Simulate `forge script script/DeployBaseSepolia.s.sol:DeployBaseSepolia` with
   the selected public sender, chain84532, and named wallet account. Omit
   `--broadcast`. Keep logs/configuration private and review the generated plan.
4. Only the authorized operator adds `--broadcast` after reviewing simulation
   results and fee policy. Preserve Foundry `broadcast/` and `cache/` state; never
   retry an ambiguous deployment by discarding nonce/transaction state.
5. Record public chain ID, registry address, transaction hash, confirmed block,
   bytecode fingerprint, and explorer verification without wallet/RPC secrets.
   Verify the receipt remains canonical under the chosen confirmation policy.

The preparation Python tool intentionally rejects signing/broadcast commands.
Installing host services, funding a wallet, invoking the future broadcast step,
and public contract verification are outside current authorization.

## Registration worker blocker

The current Go registration client remains restricted to unlocked local Anvil.
Do not point it at Base Sepolia or merely remove loopback/chain restrictions.
The [offline signed-registration layer](signed-registration-preparation.md) now
provides guarded intent preparation, durable account lanes, and confirmation
checks without any broadcaster. A separate public worker still needs the selected
custody/RPC adapter, explicit chain/contract/signer binding,
bounded fee policy, exclusive nonce ownership, durable signed transactions saved
before broadcast, restart/rebroadcast recovery, canonical confirmations/readback,
and redacted provider errors. Deploying a registry alone does not complete that
integration or establish availability of encrypted files.

## Separate local Anvil evidence

Use the existing [automatic registration guide](automatic-registration.md) and
local registry/worker tests for chain31337. Keep its development account, endpoint,
state directory, and results separate from a Base Sepolia pilot inventory. The
local browser pilot drill deliberately disables chain registration so recovery
and authorization remain independent of RPC availability.
