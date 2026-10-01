#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

mkdir -p bin data/local-demo
umask 077

# Keep the same local credentials across restarts.
if [[ ! -f .env.tank-local ]]; then
  {
    printf 'TANK_NODE_TOKEN=%s\n' "$(openssl rand -hex 32)"
    printf 'TANK_API_TOKEN=%s\n' "$(openssl rand -hex 32)"
  } > .env.tank-local
fi

set -a
source .env.tank-local
set +a

export TANK_COORDINATOR_ADDR="127.0.0.1:8080"
export TANK_DATABASE_PATH="data/local-demo/tank.sqlite"
export TANK_MAX_SEGMENT_BYTES="4194304"
export TANK_NODES="http://127.0.0.1:9101,http://127.0.0.1:9102,http://127.0.0.1:9103"

go build -o bin/tank-node ./cmd/tank-node
go build -o bin/coordinator ./cmd/coordinator

# Check that another process is not already using these ports.
python3 - <<'PY'
import socket

for port in (8080, 9101, 9102, 9103):
    with socket.socket() as sock:
        try:
            sock.bind(("127.0.0.1", port))
        except OSError:
            raise SystemExit(
                f"Port {port} is already in use. Stop its previous process first."
            )
PY

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

for index in 1 2 3; do
  TANK_NODE_ADDR="127.0.0.1:$((9100 + index))" \
  TANK_NODE_DATA_DIR="data/local-demo/node${index}" \
    ./bin/tank-node > "data/local-demo/node${index}.log" 2>&1 &

  pids+=("$!")
done

for index in 1 2 3; do
  ready=false

  for attempt in {1..50}; do
    if ! kill -0 "${pids[$((index - 1))]}" 2>/dev/null; then
      cat "data/local-demo/node${index}.log"
      exit 1
    fi

    if curl --silent --fail \
      "http://127.0.0.1:$((9100 + index))/health" >/dev/null; then
      ready=true
      break
    fi

    sleep 0.1
  done

  if [[ "$ready" != true ]]; then
    printf 'Node %s failed to become ready.\n' "$index"
    exit 1
  fi
done

./bin/coordinator &
pids+=("$!")

wait "${pids[-1]}"
