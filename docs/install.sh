#!/bin/sh
# devhost installer — https://github.com/wickedev/devhost
#
#   curl -fsSL https://wickedev.github.io/devhost/install.sh | sh
#
# Downloads the latest release binary for this OS/arch into
# $DEVHOST_INSTALL_DIR (default ~/.local/bin). No sudo required.
set -eu

REPO="wickedev/devhost"
BIN_DIR="${DEVHOST_INSTALL_DIR:-$HOME/.local/bin}"

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
  darwin|linux) ;;
  *) echo "devhost: unsupported OS: $OS (darwin and linux only)" >&2; exit 1 ;;
esac

ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64) ARCH=amd64 ;;
  arm64|aarch64) ARCH=arm64 ;;
  *) echo "devhost: unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

URL="https://github.com/$REPO/releases/latest/download/devhost_${OS}_${ARCH}.tar.gz"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

echo "devhost: downloading $URL"
curl -fsSL "$URL" -o "$TMP/devhost.tar.gz"
tar -xzf "$TMP/devhost.tar.gz" -C "$TMP"

mkdir -p "$BIN_DIR"
install -m 755 "$TMP/devhost" "$BIN_DIR/devhost"
echo "devhost: installed $BIN_DIR/devhost ($("$BIN_DIR/devhost" version))"

# Finish machine setup right here — shims, PATH, daemon service, root
# helper — so the one-liner is the whole install. DEVHOST_NO_SETUP=1 to
# only place the binary.
if [ -z "${DEVHOST_NO_SETUP:-}" ]; then
  echo
  "$BIN_DIR/devhost" setup
  echo
  echo "next: cd <project> && devhost init   # opt a project in (commit the .devhost marker)"
else
  echo
  echo "next steps:"
  echo "  $BIN_DIR/devhost setup        # shims, PATH, daemon service, root helper"
  echo "  cd <project> && devhost init  # opt a project in (commit the .devhost marker)"
fi
