#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
mkdir -p bin data/local-demo
umask 077

source scripts/init-local-env.sh

export TANK_COORDINATOR_ADDR="127.0.0.1:8080"
export TANK_DATABASE_PATH="data/local-demo/tank.sqlite"
export TANK_MAX_SEGMENT_BYTES="4194304"
export TANK_NODES="http://127.0.0.1:9101,http://127.0.0.1:9102,http://127.0.0.1:9103,http://127.0.0.1:9104"

go build -o bin/tank-node ./cmd/tank-node
go build -o bin/coordinator ./cmd/coordinator

registration_enabled=false
if [[ -n "${TANK_CHAIN_RPC:-}${TANK_REGISTRY_ADDR:-}${TANK_REGISTRANT:-}" ]]; then
  if [[ -z "${TANK_CHAIN_RPC:-}" || -z "${TANK_REGISTRY_ADDR:-}" || -z "${TANK_REGISTRANT:-}" ]]; then
    echo "Set TANK_CHAIN_RPC, TANK_REGISTRY_ADDR, and TANK_REGISTRANT together." >&2
    exit 1
  fi
  go build -o bin/tank-registration-worker ./cmd/tank-registration-worker
  registration_enabled=true
fi

pids=()

cleanup() {
  for pid in "${pids[@]}"; do
    kill "$pid" 2>/dev/null || true
  done
  for pid in "${pids[@]}"; do
    wait "$pid" 2>/dev/null || true
  done
}

trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

for index in 1 2 3 4; do
  TANK_NODE_ADDR="127.0.0.1:$((9100 + index))" \
  TANK_NODE_DATA_DIR="data/local-demo/node${index}" \
    ./bin/tank-node > "data/local-demo/node${index}.log" 2>&1 &

  pids+=("$!")
  printf '%s\n' "${pids[-1]}" > "data/local-demo/node${index}.pid"
done

sleep 1

for index in 1 2 3 4; do
  if ! kill -0 "${pids[$((index - 1))]}" 2>/dev/null; then
    cat "data/local-demo/node${index}.log"
    exit 1
  fi

  curl --fail --silent --show-error \
    "http://127.0.0.1:$((9100 + index))/health"
done

./bin/coordinator &
pids+=("$!")

if [[ "$registration_enabled" == true ]]; then
  ./bin/tank-registration-worker > data/local-demo/registration-worker.log 2>&1 &
  pids+=("$!")
  echo "Registration worker started; log: data/local-demo/registration-worker.log"
fi

# EXIT cleanup stops the remaining processes when any managed process exits.
set +e
wait -n "${pids[@]}"
status=$?
set -e

echo "A managed process exited; stopping the local stack." >&2
if [[ "$status" -eq 0 ]]; then
  status=1
fi
exit "$status"
