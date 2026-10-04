#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
umask 077
mkdir -p data/local-demo

anvil_bin="${TANK_ANVIL_BIN:-$HOME/.foundry/bin/anvil}"
if [[ ! -x "$anvil_bin" ]]; then
  echo "Anvil executable missing: $anvil_bin" >&2
  exit 1
fi

exec "$anvil_bin" --host 127.0.0.1 --port 8545 --chain-id 31337 --state "$PWD/data/local-demo/anvil-state.json" --state-interval 10
