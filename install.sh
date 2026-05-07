#!/bin/sh
# phi installer for Linux and macOS
#
# Usage:
#   curl -sSL https://phi.philtechs.org/install.sh | sh
#   # or pin a version:
#   curl -sSL .../install.sh | sh -s -- --version v0.1.0

set -eu

REPO="philtechs-org/phi"
INSTALL_DIR="${PHI_INSTALL_DIR:-/usr/local/bin}"
VERSION=""

while [ $# -gt 0 ]; do
    case "$1" in
        --version) VERSION="$2"; shift 2 ;;
        --version=*) VERSION="${1#*=}"; shift ;;
        --dir) INSTALL_DIR="$2"; shift 2 ;;
        --dir=*) INSTALL_DIR="${1#*=}"; shift ;;
        *) echo "phi-install: unknown flag $1" >&2; exit 1 ;;
    esac
done

# Detect OS + arch
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
    x86_64 | amd64) arch="x86_64" ;;
    arm64 | aarch64) arch="arm64" ;;
    *) echo "phi-install: unsupported arch: $arch" >&2; exit 1 ;;
esac
case "$os" in
    linux) os_title="Linux" ;;
    darwin) os_title="Darwin" ;;
    *) echo "phi-install: unsupported OS: $os" >&2; exit 1 ;;
esac

# Resolve version
if [ -z "$VERSION" ]; then
    VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
        | grep -o '"tag_name":[[:space:]]*"[^"]*"' | head -n1 \
        | sed 's/.*"\(v[^"]*\)".*/\1/')
    if [ -z "$VERSION" ]; then
        echo "phi-install: could not determine latest release" >&2
        exit 1
    fi
fi
ver_no_v="${VERSION#v}"

archive="phi_${ver_no_v}_${os_title}_${arch}.tar.gz"
url="https://github.com/${REPO}/releases/download/${VERSION}/${archive}"
sums_url="https://github.com/${REPO}/releases/download/${VERSION}/checksums.txt"

echo "phi-install: downloading ${archive}"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

curl -fsSL "$url" -o "$tmp/$archive"
curl -fsSL "$sums_url" -o "$tmp/checksums.txt"

# Verify checksum
expected=$(grep "  $archive\$" "$tmp/checksums.txt" | awk '{print $1}')
if [ -z "$expected" ]; then
    echo "phi-install: $archive not in checksums.txt" >&2
    exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
    actual=$(sha256sum "$tmp/$archive" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
    actual=$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')
else
    echo "phi-install: no sha256 tool found (need sha256sum or shasum)" >&2
    exit 1
fi
if [ "$expected" != "$actual" ]; then
    echo "phi-install: checksum mismatch for $archive" >&2
    echo "  expected: $expected" >&2
    echo "  actual:   $actual" >&2
    exit 1
fi

echo "phi-install: checksum OK"

tar -xzf "$tmp/$archive" -C "$tmp"

# Install
target="$INSTALL_DIR/phi"
if [ -w "$INSTALL_DIR" ] || [ "$(id -u)" = "0" ]; then
    install -m 0755 "$tmp/phi" "$target"
else
    sudo install -m 0755 "$tmp/phi" "$target"
fi

echo "phi-install: installed phi $VERSION at $target"
"$target" version
