#!/bin/sh
set -eu
umask 077

# First install only. After that, darkrun bootstrap owns updates and recovery.
root=${DARKSPIN_BOOTSTRAP_PATH:-/darkspin}
case "$root" in /*) ;; *) echo 'Bootstrap path must be absolute' >&2; exit 1;; esac
if [ -L "$root" ] || [ -L "$root/bin" ] || [ -L "$root/bin/darkrun" ]; then
  echo 'Bootstrap paths must not be symbolic links' >&2
  exit 1
fi
mkdir -p "$root/bin"
binary="$root/bin/darkrun"
if [ ! -e "$binary" ]; then
  seed=$(mktemp -d "$root/bin/seed.XXXXXXXX")
  cleanup() {
    rm -f "$seed/manifest.zip" "$seed/manifest.json" "$seed/binary.zip" "$seed/darkrun" "$seed/identity.json"
    rmdir "$seed"
  }
  trap cleanup EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
  manifest_url=${DARKSPIN_UPDATE_MANIFEST_URL:-https://nightly.link/darkspinnet/darkspin/workflows/main-build/main/darkrun-update-linux-amd64-unstable.zip}
  fetch() {
    curl --fail --silent --show-error --location --max-redirs 5 \
      --proto '=https' --proto-redir '=https' --connect-timeout 15 --max-time 180 \
      --max-filesize "$3" --output "$2" "$1"
  }
  fetch "$manifest_url" "$seed/manifest.zip" 1048576
  if jq -e 'type == "object"' "$seed/manifest.zip" >/dev/null 2>&1; then
    cp "$seed/manifest.zip" "$seed/manifest.json"
  else
    [ "$(unzip -Z1 "$seed/manifest.zip")" = darkrun-update.json ] || { echo 'Unexpected manifest archive' >&2; exit 1; }
    unzip -p "$seed/manifest.zip" darkrun-update.json | head -c 65537 > "$seed/manifest.json"
  fi
  [ "$(wc -c < "$seed/manifest.json")" -le 65536 ] || { echo 'Manifest exceeds size limit' >&2; exit 1; }
  jq -e '.platform == "linux-amd64" and .format == "zip" and
    (.version | type == "string" and length > 0) and
    (.build_id | type == "string" and length > 0 and length <= 256) and
    (.sha256 | type == "string" and test("^[a-fA-F0-9]{64}$")) and
    (.url | type == "string" and test("^https://[^/@#]+/[^#]*$"))' "$seed/manifest.json" >/dev/null
  download_url=$(jq -r .url "$seed/manifest.json")
  fetch "$download_url" "$seed/binary.zip" 269484032
  [ "$(unzip -Z1 "$seed/binary.zip")" = darkrun ] || { echo 'Unexpected binary archive' >&2; exit 1; }
  unzip -p "$seed/binary.zip" darkrun | head -c 268435457 > "$seed/darkrun"
  [ "$(wc -c < "$seed/darkrun")" -le 268435456 ] && [ -s "$seed/darkrun" ] || { echo 'Invalid binary size' >&2; exit 1; }
  expected=$(jq -r '.sha256 | ascii_downcase' "$seed/manifest.json")
  actual=$(sha256sum "$seed/darkrun")
  [ "${actual%% *}" = "$expected" ] || { echo 'Binary checksum mismatch' >&2; exit 1; }
  chmod 755 "$seed/darkrun"
  timeout 10 "$seed/darkrun" build-info > "$seed/identity.json"
  jq -e --slurpfile expected "$seed/manifest.json" \
    '.build_id == $expected[0].build_id and .version == $expected[0].version and .platform == $expected[0].platform' \
    "$seed/identity.json" >/dev/null
  mv "$seed/darkrun" "$binary"
  cleanup
  trap - EXIT INT TERM
fi
[ -f "$binary" ] && [ -x "$binary" ] || { echo 'Installed darkrun is not executable' >&2; exit 1; }
if [ "$#" -eq 0 ]; then set -- bootstrap; fi
exec "$binary" "$@"
