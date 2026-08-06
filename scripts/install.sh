#!/usr/bin/env sh
# Installs a pinned overwater release after verifying its checksum
# against the release's SHA256SUMS. Download this script, read it, then
# run it; never pipe curl straight into sh.
#
# Usage: sh install.sh vX.Y.Z [install-dir]
set -eu

version="${1:?usage: sh install.sh vX.Y.Z [install-dir]}"
dir="${2:-$HOME/.local/bin}"

case "$(uname -s)" in
  Darwin) goos=darwin ;;
  Linux) goos=linux ;;
  *) echo "unsupported OS: $(uname -s)" >&2; exit 2 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) goarch=amd64 ;;
  arm64|aarch64) goarch=arm64 ;;
  *) echo "unsupported arch: $(uname -m)" >&2; exit 2 ;;
esac

bin="overwater_${goos}_${goarch}"
base="https://github.com/MithrilBytes/overwater/releases/download/${version}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

curl -fsSL -o "$tmp/$bin" "$base/$bin"
curl -fsSL -o "$tmp/SHA256SUMS" "$base/SHA256SUMS"
(
  cd "$tmp"
  line="$(grep " ${bin}\$" SHA256SUMS)"
  if command -v sha256sum > /dev/null 2>&1; then
    echo "$line" | sha256sum -c -
  else
    echo "$line" | shasum -a 256 -c -
  fi
)

mkdir -p "$dir"
install -m 0755 "$tmp/$bin" "$dir/overwater"
echo "installed overwater $version to $dir/overwater"
# Releases before v2.0.0 predate the version subcommand.
"$dir/overwater" version 2> /dev/null || true
