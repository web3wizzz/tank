#!/usr/bin/env bash
set -euo pipefail
umask 077
rpc=http://127.0.0.1:8545
registry=0x5FbDB2315678afecb367f032d93F642f64180aa3
sender=0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266
# Verify dynamic loader/runtime support without printing development keys.
arc-anvil --version
arc-cast --version
[[ -r /fixture/creation.txt && -r /fixture/runtime.txt && -w /chain ]] || {
 echo 'Fixture files must be readable and the owned chain volume writable.' >&2; exit 1;
}
# Redirect Anvil's development-account banner; never emit private fixture keys.
arc-anvil --host 127.0.0.1 --port 8545 --chain-id 31337 \
  --state /chain/state.json --state-interval 1 > /tmp/local-chain.log 2>&1 &
child=$!
trap 'kill "$child" 2>/dev/null || true; wait "$child" 2>/dev/null || true' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
ready=false
for attempt in {1..100}; do
  kill -0 "$child" 2>/dev/null || { echo 'Owned fixture chain exited.' >&2; exit 1; }
  if [[ "$(arc-cast chain-id --rpc-url "$rpc" 2>/dev/null || true)" == 31337 ]]; then
    ready=true; break
  fi
  sleep 0.1
done
[[ "$ready" == true ]] || { echo 'Fixture chain startup timed out.' >&2; exit 1; }
code="$(arc-cast code "$registry" --rpc-url "$rpc")"
if [[ "$code" == 0x ]]; then
  [[ "$(arc-cast nonce "$sender" --rpc-url "$rpc")" == 0 ]] || {
    echo 'Fixture nonce changed without expected registry; preserving state.' >&2; exit 1;
  }
  creation="$(cat /fixture/creation.txt)"
  arc-cast send --unlocked --from "$sender" --rpc-url "$rpc" \
    --create "$creation" > /tmp/local-deployment.json
  code="$(arc-cast code "$registry" --rpc-url "$rpc")"
fi
[[ "$code" == "$(cat /fixture/runtime.txt)" ]] || {
  echo 'Fixture registry runtime mismatch; preserving state.' >&2; exit 1;
}
touch /tmp/registry-ready
echo 'PASS: local chain and registry fixture ready (31337); no public RPC configured.'
wait "$child"
