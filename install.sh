#!/usr/bin/env bash
set -euo pipefail

prefix=/usr/local
from=
dry=0
while (($#)); do
  case "$1" in
    --from) (($# >= 2)) || { echo 'error: --from needs a path' >&2; exit 2; }; from=$2; shift 2 ;;
    --prefix) (($# >= 2)) || { echo 'error: --prefix needs a path' >&2; exit 2; }; prefix=$2; shift 2 ;;
    --dry-run) dry=1; shift ;;
    -h|--help) echo 'Usage: install.sh [--from DIR] [--prefix DIR] [--dry-run]'; exit 0 ;;
    *) echo "error: unknown option: $1" >&2; exit 2 ;;
  esac
done
[[ "$prefix" == /* ]] || { echo 'error: --prefix must be absolute' >&2; exit 2; }
if [[ -n "$from" ]]; then
  if [[ -f "$from" ]]; then
    tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
    tar -xzf "$from" -C "$tmp"
    from=$(find "$tmp" -mindepth 1 -maxdepth 1 -type d -print -quit)
  fi
  args=(--from "$from" --prefix "$prefix")
  ((dry)) && args+=(--dry-run)
  exec bash "$from/installer/install.sh" "${args[@]}"
fi
command -v curl >/dev/null || { echo 'error: curl is required' >&2; exit 1; }
command -v sha256sum >/dev/null || { echo 'error: sha256sum is required' >&2; exit 1; }
case "$(uname -m)" in x86_64) arch=amd64;; aarch64) arch=arm64;; *) echo 'error: unsupported architecture' >&2; exit 1;; esac
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
artifact="InfraPilot_linux_${arch}.tar.gz"
base='https://github.com/iAghaTraker/InfraPilot/releases/latest/download'
curl --fail --location --proto '=https' --tlsv1.2 -o "$tmp/$artifact" "$base/$artifact"
curl --fail --location --proto '=https' --tlsv1.2 -o "$tmp/checksums.txt" "$base/checksums.txt"
expected=$(awk -v f="$artifact" '$2 == f {print $1}' "$tmp/checksums.txt")
[[ "$expected" =~ ^[[:xdigit:]]{64}$ ]] || { echo 'error: missing release checksum' >&2; exit 1; }
actual=$(sha256sum "$tmp/$artifact" | awk '{print $1}')
[[ "$actual" == "$expected" ]] || { echo 'error: release checksum verification failed' >&2; exit 1; }
tar -xzf "$tmp/$artifact" -C "$tmp"
root=$(find "$tmp" -mindepth 1 -maxdepth 1 -type d -print -quit)
exec bash "$root/installer/install.sh" --from "$root" --prefix "$prefix"
