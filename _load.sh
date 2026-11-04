#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO="https://github.com/avil13/dummy-updater"
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
if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required" >&2
  exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
  HASH=(sha256sum)
elif command -v shasum >/dev/null 2>&1; then
  HASH=(shasum -a 256)
else
  echo "sha256sum or shasum is required" >&2
  exit 1
fi
BINARY="updater-$OS-$ARCH"
DEST="$ROOT/dist/$BINARY"
CURL=(curl --fail --location --silent --show-error --retry 3 --connect-timeout 15 --max-time 300)
LATEST="$("${CURL[@]}" --output /dev/null --write-out '%{url_effective}' "$REPO/releases/latest")"
case "$LATEST" in
  "$REPO/releases/tag/"?*) TAG="${LATEST#"$REPO/releases/tag/"}" ;;
  *) echo "Could not determine the latest release" >&2; exit 1 ;;
esac
mkdir -p "$ROOT/dist"
TMP="$(mktemp -d "$ROOT/dist/.load-XXXXXX")"
trap 'rm -rf "$TMP"' EXIT
# Pin both downloads to one release, even if latest changes during the download.
URL="$REPO/releases/download/$TAG"
"${CURL[@]}" --output "$TMP/SHA256SUMS" "$URL/SHA256SUMS"
EXPECTED="$(awk -v name="$BINARY" '$2 == name { print $1 }' "$TMP/SHA256SUMS")"
if [[ ! "$EXPECTED" =~ ^[0-9a-f]{64}$ ]]; then
  echo "Missing or invalid SHA256 checksum for $BINARY in $TAG" >&2
  exit 1
fi
if [[ -f "$DEST" ]] && [[ "$("${HASH[@]}" "$DEST" | awk '{ print $1 }')" == "$EXPECTED" ]]; then
  chmod +x "$DEST"
  echo "$BINARY is already up to date ($TAG)"
  exit 0
fi
echo "Downloading $BINARY ($TAG)..."
"${CURL[@]}" --output "$TMP/$BINARY" "$URL/$BINARY"
ACTUAL="$("${HASH[@]}" "$TMP/$BINARY" | awk '{ print $1 }')"
if [[ "$ACTUAL" != "$EXPECTED" ]]; then
  echo "SHA256 checksum mismatch for $BINARY; existing binary was not changed" >&2
  exit 1
fi
chmod +x "$TMP/$BINARY"
mv -f "$TMP/$BINARY" "$DEST"
echo "Installed $TAG: $DEST"
