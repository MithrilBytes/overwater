#!/usr/bin/env sh
# Installs a pinned overwater release. SHA256SUMS ships from the same
# release as the binary, so on its own it catches a corrupted download
# and nothing else; the signed build provenance is what catches a
# replaced asset, and gh, where it exists, checks it. Set
# OVERWATER_SHA256 to a digest obtained somewhere other than this
# release to check against that instead. Download this script, read it,
# then run it; never pipe curl straight into sh.
#
# Usage: sh install.sh vX.Y.Z [install-dir]
set -eu

version="${1:?usage: sh install.sh vX.Y.Z [install-dir]}"
dir="${2:-$HOME/.local/bin}"
repo="MithrilBytes/overwater"

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
base="https://github.com/${repo}/releases/download/${version}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

curl -fsSL -o "$tmp/$bin" "$base/$bin"

expected="${OVERWATER_SHA256:-}"
if [ -z "$expected" ]; then
  curl -fsSL -o "$tmp/SHA256SUMS" "$base/SHA256SUMS"
  # sha256sum writes two spaces before the name, or a space and an
  # asterisk in binary mode.
  line="$(grep -E "^[0-9a-fA-F]{64} [ *]${bin}\$" "$tmp/SHA256SUMS" || true)"
  if [ -z "$line" ]; then
    echo "the SHA256SUMS published with $version has no line for $bin" >&2
    exit 2
  fi
  expected="${line%% *}"
fi
expected="$(printf %s "$expected" | tr 'A-F' 'a-f')"

if command -v sha256sum > /dev/null 2>&1; then
  got="$(sha256sum "$tmp/$bin")"
else
  got="$(shasum -a 256 "$tmp/$bin")"
fi
got="${got%% *}"
if [ "$got" != "$expected" ]; then
  echo "checksum mismatch for $bin: got $got, want $expected" >&2
  exit 2
fi

# A digest that travelled with the binary it certifies proves only that
# the bytes arrived intact: whoever can replace the one can replace the
# other. The provenance is signed on the release path and is the check
# that survives that, so a gh that fails it stops the install.
if command -v gh > /dev/null 2>&1; then
  if ! gh attestation verify "$tmp/$bin" --repo "$repo"; then
    echo "build provenance for $bin did not verify; not installing" >&2
    exit 2
  fi
elif [ -z "${OVERWATER_SHA256:-}" ]; then
  echo "gh is not installed, so only the release's own SHA256SUMS was checked;" >&2
  echo "install gh to verify build provenance, or set OVERWATER_SHA256 out of band" >&2
fi

mkdir -p "$dir"
install -m 0755 "$tmp/$bin" "$dir/overwater"
echo "installed overwater $version to $dir/overwater"
# Releases before v2.0.0 predate the version subcommand.
"$dir/overwater" version 2> /dev/null || true
