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

case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) echo "devhost: note — $BIN_DIR is not on your PATH; add it to your shell profile" ;;
esac

echo
echo "next steps:"
echo "  devhost setup                 # install shims; prints the PATH line to add"
echo "  cd <project> && devhost init  # opt a project in (commit the .devhost marker)"
