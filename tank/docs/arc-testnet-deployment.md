# Arc Testnet deployment and registration

The user authorized TankRegistry deployment and registration on **Arc Testnet
only**, once prerequisites pass. Mainnet, paid hosting and real funds remain
unauthorized. This supersedes the historical preparation-only restriction.
No deployed address or public transaction is claimed until canonical verification
passes. Official parameters were rechecked on 2026-10-12:
[connection](https://docs.arc.io/arc/references/connect-to-arc) and
[fees](https://docs.arc.io/arc/references/gas-and-fees). Testnet is chain 5042002;
Arc Mainnet 5042 and all unexpected chains are rejected before signing/submission.
Native gas uses USDC with 18 decimals.

## Secure local setup

Run these commands **in your local terminal**, from `/workspaces/tank/tank`.
Never paste passwords, raw private keys, recovery keys or authenticated RPC URLs
into chat. The existing encrypted-keystore adapter signs on the worker host;
it is not a remote signing-service adapter. A remote service requires a selected
API and private authentication configuration before that custody can be tested.
Use a newly generated dedicated testnet account, exclusively with one deployment
journal and one metadata journal. Never import an existing funded/mainnet account.

The example deployment ceiling is 0.04 testnet USDC, with a separate 0.10 testnet
USDC aggregate registration ceiling. They are explicit ceilings, not current fee
quotes. Refusal at these caps requires operator review; tools never raise caps.
The example account needs enough faucet funds for these costs.

```bash
cd /workspaces/tank/tank
set +x
umask 077
mkdir -m 700 data/arc-testnet
/tmp/tank-arc-foundry/arc-cast wallet new data/arc-testnet tank-testnet
# Enter a NEW password at the hidden terminal prompt. Encrypted mode prints
# the public address, not the private key. Do not invoke wallet new without PATH.
chmod 600 data/arc-testnet/tank-testnet
python3 - <<'PY'
import getpass, os
password = getpass.getpass('Re-enter the new testnet keystore password: ')
if not password:
    raise SystemExit('An empty password is not accepted.')
fd = os.open('data/arc-testnet/password', os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
with os.fdopen(fd, 'w') as f:
    f.write(password + '\n')
PY
export TANK_CHAIN_ID=5042002
export TANK_SIGNER_KEYSTORE_FILE="$PWD/data/arc-testnet/tank-testnet"
export TANK_SIGNER_PASSWORD_FILE="$PWD/data/arc-testnet/password"
export TANK_ARC_TESTNET_SENDER="$(python3 - <<'PY'
import json
with open('data/arc-testnet/tank-testnet') as f:
    print('0x' + json.load(f)['address'].removeprefix('0x'))
PY
)"
# Public reference endpoint. This environment received HTTP 403 from it.
export TANK_ARC_TESTNET_RPC=https://rpc.testnet.arc.io
# To select another HTTPS RPC securely, use a hidden prompt, never a shell
# command containing its secret URL:
# read -r -s -p 'Selected HTTPS testnet RPC: ' TANK_ARC_TESTNET_RPC
# export TANK_ARC_TESTNET_RPC

export TANK_ARC_DEPLOY_MAX_GAS=1000000
export TANK_ARC_DEPLOY_MAX_FEE=40000000000
export TANK_ARC_DEPLOY_MAX_TIP=1000000000
export TANK_ARC_DEPLOY_MAX_COST=40000000000000000
export TANK_ARC_DEPLOY_ARTIFACT="$PWD/contracts/out/TankRegistry.sol/TankRegistry.json"
export TANK_ARC_DEPLOY_JOURNAL="$PWD/data/arc-testnet/deployment.raw"
export TANK_ARC_TESTNET_MAX_GAS=200000
export TANK_ARC_TESTNET_MAX_FEE=40000000000
export TANK_ARC_TESTNET_MAX_TIP=1000000000
export TANK_ARC_TESTNET_MAX_COST=8000000000000000
export TANK_ARC_TESTNET_TOTAL_BUDGET=100000000000000000
export TANK_ARC_TESTNET_CONFIRMATIONS=1
/tmp/tank-arc-foundry/arc-forge build --root contracts
go build -o bin/tank-deploy-arc ./cmd/tank-deploy-arc
go build -o bin/tank-prepare-registration ./cmd/tank-prepare-registration
# Persist selected settings privately so later terminal/agent sessions can load
# them. This includes any secret provider URL, but never the password contents.
python3 - <<'PY'
import os, shlex
names = ['TANK_CHAIN_ID', 'TANK_SIGNER_KEYSTORE_FILE', 'TANK_SIGNER_PASSWORD_FILE',
         'TANK_ARC_TESTNET_SENDER', 'TANK_ARC_TESTNET_RPC', 'TANK_ARC_DEPLOY_MAX_GAS',
         'TANK_ARC_DEPLOY_MAX_FEE', 'TANK_ARC_DEPLOY_MAX_TIP', 'TANK_ARC_DEPLOY_MAX_COST',
         'TANK_ARC_DEPLOY_ARTIFACT', 'TANK_ARC_DEPLOY_JOURNAL', 'TANK_ARC_TESTNET_MAX_GAS',
         'TANK_ARC_TESTNET_MAX_FEE', 'TANK_ARC_TESTNET_MAX_TIP', 'TANK_ARC_TESTNET_MAX_COST',
         'TANK_ARC_TESTNET_TOTAL_BUDGET', 'TANK_ARC_TESTNET_CONFIRMATIONS']
fd = os.open('data/arc-testnet/worker.env', os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
with os.fdopen(fd, 'w') as f:
    for name in names:
        f.write('export ' + name + '=' + shlex.quote(os.environ[name]) + '\n')
PY
```

Request **Arc Testnet USDC only** from the official
[Circle faucet](https://faucet.circle.com), using the generated public address.
Do not buy funds, bridge real assets or provision a paid RPC. Then run:

```bash
python3 scripts/prepare-arc-testnet.py check-rpc
bin/tank-deploy-arc preflight
# Authorized only after preflight passes with the dedicated testnet account.
bin/tank-deploy-arc deploy
```

Preflight decrypts the private keystore and checks the expected address, HTTPS
RPC chain, current gas estimate with 20% margin, current gas price doubled
(at least Arc's floor), native balance and every configured ceiling. No signing
or submission occurs in preflight. The `deploy` command rechecks prerequisites,
signs a zero-value type-2 creation, durably journals bytes before send, and locks
the private journal against concurrent deployment. Never remove/replace that
journal to retry. Rerun the same `deploy` command after timeout: it resumes the
same nonce/hash/bytes, observes an existing receipt before rebroadcast, and never
creates a replacement deployment transaction. Do not use this account with other
tools or workers concurrently. Retain the journal and latest private backups.

Success prints public JSON containing address, chain ID, transaction hash,
maximum fee reservation and the official explorer link. `deployed: true` means
a successful canonical receipt and exact compiled runtime bytecode equality at
the receipt block, followed by canonical block/network rechecks. A transaction
hash printed on timeout is pending evidence; it is not successful deployment.
Record the public JSON in a deployment acceptance report after verification.

## Worker configuration and acceptance

Set the verified public address and the private metadata database used by the
coordinator. A newly empty database cannot register existing uploaded files.
Back up the actual database with `tank-backup` before its additive schema8
upgrade. Preserve users, access credentials, filenames, manifests and journals.
Do not point a worker at a copy while another worker uses the same signer.

```bash
read -r -p 'Verified public TankRegistry address: ' TANK_REGISTRY_ADDR
export TANK_REGISTRY_ADDR
read -r -p 'Absolute private coordinator metadata database path: ' TANK_DATABASE_PATH
export TANK_DATABASE_PATH
# Append the selected registry and database to the same private settings file.
python3 - <<'PY'
import os, shlex
with open('data/arc-testnet/worker.env', 'a') as f:
    for name in ['TANK_REGISTRY_ADDR', 'TANK_DATABASE_PATH']:
        f.write('export ' + name + '=' + shlex.quote(os.environ[name]) + '\n')
PY
# Database parent must be owned by this user and mode0700; no symlink DB/WAL/SHM.
bin/tank-prepare-registration --broadcast
```

In later sessions, use `set +x; source data/arc-testnet/worker.env` from `tank/`.
Share only that local configuration path with the agent, never its contents.

For the coordinator's existing private service configuration, set these public
registration settings alongside its existing database/node/access settings:

```bash
export TANK_REGISTRATION_MODE=arc-testnet-external
export TANK_CHAIN_ID=5042002
# Use the same verified TANK_REGISTRY_ADDR and TANK_ARC_TESTNET_SENDER above.
```

The coordinator reports the external worker's namespaced registration status
from their shared metadata database. It holds no signer and makes no RPC calls
in this mode. Do not copy signing/password files or secret RPC settings to the
coordinator. Existing local mode remains available for isolated Anvil work.

This command performs one bounded pass. Repeat it on a timer to process later
uploads and due retries, maintaining the same private environment and account.
Without `--broadcast`, preparation remains non-broadcasting. Authorized mode
verifies exact compiled registry runtime before processing. It reserves the
transaction's maximum cost atomically in the durable account budget before send.
Reservations survive confirmation, retries, restart and backups. Repeated hashes
are charged once; changed budget settings fail closed rather than silently raising
the previously selected ceiling. This conservative registration budget is separate
from deployment and excludes activity outside this dedicated journal/account.

Jobs complete only after a successful canonical receipt and matching commitment
and ciphertext size read at that receipt's canonical block hash. Missing receipts,
ambiguous RPC errors and failed readback retain the original transaction and nonce
lane. Backoff starts at 30 seconds and caps at 15 minutes. Reverts and nonce
discrepancies require review; do not delete state or increase fees automatically.

Real acceptance must upload a fresh browser-encrypted file, process its registration,
record the public registration hash, verify canonical commitment/size readback,
retrieve ciphertext and decrypt locally, compare original bytes and Unicode
filename, and verify another user's download is denied. Stop/restart the worker
between submission and completion to demonstrate journal recovery. Local tests
exercise these components separately; they do not substitute for the full testnet
encrypted-file acceptance or a selected remote signer.

Arc stores commitments only. Frontend/coordinator HTTPS hosting, three independent
active storage machines plus spare, per-node credentials, storage capacity,
backups/restore, monitoring, retention/funding/operator-exit terms, and actual
machine-failure acceptance remain separate requirements. No paid infrastructure
is authorized. See [pilot operations](pilot-operations.md).
