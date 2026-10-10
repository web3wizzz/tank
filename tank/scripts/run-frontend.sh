#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

source scripts/init-local-env.sh

: "${TANK_SESSION_SECRET:?TANK_SESSION_SECRET is missing}"
export TANK_API_URL="${TANK_API_URL:-http://127.0.0.1:8080}"

unset TANK_API_TOKEN TANK_NODE_TOKEN
cd frontend
exec npm run start -- --hostname 0.0.0.0
