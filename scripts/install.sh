#!/bin/sh
# Lenos Installer Bootstrap
# Usage: curl -fsSL https://lenos.sh/install | sh
#
# Detects OS/arch, downloads the lenos-installer binary, and runs it.
# The installer handles downloading all ecosystem tools and writing configs.

set -eu

ORG="tta-lab"
REPO="lenos"
INSTALLER="lenos-installer"
BIN_DIR="${HOME}/.local/bin"

OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
	Linux)  GOOS="Linux" ;;
	Darwin) GOOS="Darwin" ;;
	*)
		echo "error: unsupported OS: $OS" >&2
		exit 1
		;;
esac

case "$ARCH" in
	x86_64|amd64) GOARCH="x86_64" ;;
	arm64|aarch64) GOARCH="arm64" ;;
	*)
		echo "error: unsupported arch: $ARCH" >&2
		exit 1
		;;
esac

VERSION="latest"
if [ "${1:-}" != "" ]; then
	VERSION="$1"
fi

if [ "$VERSION" = "latest" ]; then
	RELEASE_URL="https://github.com/${ORG}/${REPO}/releases/latest/download"
else
	RELEASE_URL="https://github.com/${ORG}/${REPO}/releases/download/${VERSION}"
fi

ARCHIVE="${INSTALLER}_${GOOS}_${GOARCH}.tar.gz"
DOWNLOAD_URL="${RELEASE_URL}/${ARCHIVE}"

echo "→ Lenos installer bootstrap"
echo "  os/arch: $GOOS/$GOARCH"

mkdir -p "$BIN_DIR"

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

if command -v curl >/dev/null 2>&1; then
	curl -fsSL "$DOWNLOAD_URL" -o "${TMPDIR}/${ARCHIVE}"
elif command -v wget >/dev/null 2>&1; then
	wget -q "$DOWNLOAD_URL" -O "${TMPDIR}/${ARCHIVE}"
else
	echo "error: neither curl nor wget found" >&2
	exit 1
fi

tar -xzf "${TMPDIR}/${ARCHIVE}" -C "$BIN_DIR" "$INSTALLER"
chmod +x "${BIN_DIR}/${INSTALLER}"

exec "${BIN_DIR}/${INSTALLER}" "$@"
