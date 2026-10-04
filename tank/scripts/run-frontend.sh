#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

if [[ ! -f .env.tank-local ]]; then
  echo "Missing .env.tank-local. Start scripts/run-local.sh first."
  exit 1
fi

set -a
source .env.tank-local
set +a

: "${TANK_API_TOKEN:?TANK_API_TOKEN is missing}"
export TANK_API_URL="${TANK_API_URL:-http://127.0.0.1:8080}"

cd frontend
exec npm run start -- --hostname 0.0.0.0
