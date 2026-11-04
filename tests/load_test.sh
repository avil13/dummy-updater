#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/bin" "$TMP/repo with spaces" "$TMP/elsewhere"
cp "$ROOT/_load.sh" "$TMP/repo with spaces/"
export FIXTURES="$TMP" FAKE_OS=Linux FAKE_ARCH=aarch64
export PATH="$TMP/bin:$PATH"
cat > "$TMP/bin/uname" <<'SH'
#!/usr/bin/env bash
case "$1" in
  -s) echo "$FAKE_OS" ;;
  -m) echo "$FAKE_ARCH" ;;
esac
SH
cat > "$TMP/bin/curl" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
OUT=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --output) OUT="$2"; shift 2 ;;
    --write-out|--retry|--connect-timeout|--max-time) shift 2 ;;
    --*) shift ;;
    *) URL="$1"; shift ;;
  esac
done
echo "$URL" >> "$FIXTURES/requests"
case "$URL" in
  https://github.com/avil13/dummy-updater/releases/latest)
    printf 'https://github.com/avil13/dummy-updater/releases/tag/v1.0.1' ;;
  https://github.com/avil13/dummy-updater/releases/download/v1.0.1/SHA256SUMS)
    cp "$FIXTURES/SHA256SUMS" "$OUT" ;;
  https://github.com/avil13/dummy-updater/releases/download/v1.0.1/updater-*)
    if [[ "${FAIL_DOWNLOAD:-}" == network ]]; then
      printf 'partial download' > "$OUT"
      exit 22
    fi
    cp "$FIXTURES/payload" "$OUT" ;;
  *) echo "Unexpected URL: $URL" >&2; exit 1 ;;
esac
SH
chmod +x "$TMP/bin/"*
printf 'release binary\n' > "$TMP/payload"
if command -v sha256sum >/dev/null 2>&1; then
  SUM="$(sha256sum "$TMP/payload" | awk '{ print $1 }')"
else
  SUM="$(shasum -a 256 "$TMP/payload" | awk '{ print $1 }')"
fi
for os in linux darwin; do
  for arch in amd64 arm64; do
    printf '%s  updater-%s-%s\n' "$SUM" "$os" "$arch"
  done
done > "$TMP/SHA256SUMS"
cd "$TMP/elsewhere"
for platform in 'Linux x86_64 linux-amd64' 'Linux aarch64 linux-arm64' 'Darwin x86_64 darwin-amd64' 'Darwin arm64 darwin-arm64'; do
  read -r FAKE_OS FAKE_ARCH NAME <<< "$platform"
  DEST="$TMP/repo with spaces/dist/updater-$NAME"
  bash "$TMP/repo with spaces/_load.sh"
  cmp "$TMP/payload" "$DEST"
  test -x "$DEST"
  BEFORE="$(grep -c '/download/.*/updater-' "$TMP/requests")"
  bash "$TMP/repo with spaces/_load.sh" | grep -q 'already up to date'
  test "$(grep -c '/download/.*/updater-' "$TMP/requests")" = "$BEFORE"
done
# Failed or corrupt downloads must preserve the previously installed binary.
printf 'new release binary\n' > "$TMP/payload"
printf '%064d  updater-darwin-arm64\n' 0 > "$TMP/SHA256SUMS"
for failure in network checksum; do
  export FAIL_DOWNLOAD="$failure"
  if bash "$TMP/repo with spaces/_load.sh" > "$TMP/output" 2>&1; then
    echo "Expected $failure failure" >&2
    exit 1
  fi
  test "$(< "$DEST")" = 'release binary'
done
unset FAIL_DOWNLOAD
printf 'invalid checksum\n' > "$TMP/SHA256SUMS"
if bash "$TMP/repo with spaces/_load.sh" > "$TMP/output" 2>&1; then
  echo 'Expected invalid checksum failure' >&2
  exit 1
fi
export FAKE_ARCH=unsupported
if bash "$TMP/repo with spaces/_load.sh" > "$TMP/output" 2>&1; then
  echo 'Expected unsupported architecture failure' >&2
  exit 1
fi
test -z "$(find "$TMP/repo with spaces/dist" -name '.load-*' -print)"
echo 'Loader tests passed'
