#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
case "$(uname -s)" in
  Linux) OS=linux ;;
  Darwin) OS=darwin ;;
  *) echo "Unsupported operating system" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "Unsupported architecture" >&2; exit 1 ;;
esac
if [[ "${1:-}" == add || "${1:-}" == stop ]]; then
  ACTION="$1"
  shift
  exec "$ROOT/dist/updater-$OS-$ARCH" "$ACTION" -config "$ROOT/config.json" "$@"
fi
exec "$ROOT/dist/updater-$OS-$ARCH" -config "$ROOT/config.json" "$@"
