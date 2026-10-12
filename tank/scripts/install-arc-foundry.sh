#!/usr/bin/env bash
# Official Linux x86_64 release, pinned and digest-checked. No wallet/network setup.
set -euo pipefail
umask 077
[[ $# == 1 && "$(uname -s)" == Linux && "$(uname -m)" == x86_64 ]] || {
  echo 'Usage: install-arc-foundry.sh PRIVATE_NEW_DIRECTORY (Linux x86_64)' >&2
  exit 1
}
[[ ! -e "$1" ]] || { echo 'Tool directory must be new.' >&2; exit 1; }
mkdir -m 700 -p "$1"
target="$(cd "$1" && pwd)"
curl --fail --silent --show-error --location \
  https://github.com/circlefin/arc-foundry/releases/download/v0.8.0-2/arc-foundry-v0.8.0-2-x86_64-unknown-linux-gnu.tar.gz \
  --output "$target/release.tar.gz"
printf '%s  %s\n' 088bdb96a84418b757f9825d491e702792f1d1d1e29a9145af305a6600a79556 "$target/release.tar.gz" | sha256sum --check --status
tar -xzf "$target/release.tar.gz" -C "$target"
for tool in forge cast anvil; do
  mv "$target/$tool" "$target/arc-$tool"
done
"$target/arc-forge" --version
