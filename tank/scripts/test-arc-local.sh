#!/usr/bin/env bash
# Tests create their own loopback Arc node; no public RPC/fork/wallet is used.
set -euo pipefail
cd "$(dirname "$0")/.."
: "${TANK_ARC_FORGE_BIN:?Select the pinned Arc Forge binary}"
: "${TANK_ARC_ANVIL_BIN:?Select the pinned Arc Anvil binary}"
[[ -x "$TANK_ARC_FORGE_BIN" && -x "$TANK_ARC_ANVIL_BIN" ]]
version="$("$TANK_ARC_FORGE_BIN" --version)"
[[ "$version" == *d497beea7096ff2a8e583c8b307941f24a61b06b* ]] || {
  echo 'Unexpected Arc Foundry version; use the pinned installer.' >&2
  exit 1
}
anvil_version="$("$TANK_ARC_ANVIL_BIN" --version)"
[[ "$anvil_version" == *d497beea7096ff2a8e583c8b307941f24a61b06b* ]] || {
  echo 'Unexpected Arc Anvil version.' >&2
  exit 1
}
(cd contracts; "$TANK_ARC_FORGE_BIN" fmt --check; "$TANK_ARC_FORGE_BIN" test --network arc)
go test -race -tags arc_integration -run TestArcLocal -count=1 -timeout=2m ./internal/registry
