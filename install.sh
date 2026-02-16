#!/usr/bin/env sh
set -eu

REPO="Obmondo/kubeaid-cli"
BIN_NAME="kubeaid-cli"
INSTALL_DIR="/usr/local/bin"

fail() {
  printf "%s\n" "$1" >&2
  exit 1
}

if command -v curl >/dev/null 2>&1; then
  VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | awk -F '"' '/"tag_name":/ { print $4; exit }')"
elif command -v wget >/dev/null 2>&1; then
  VERSION="$(wget -qO- "https://api.github.com/repos/${REPO}/releases/latest" | awk -F '"' '/"tag_name":/ { print $4; exit }')"
else
  fail "curl or wget is required to fetch the latest version"
fi

[ -n "$VERSION" ] || fail "Could not resolve kubeaid-cli version"

UNAME_S="$(uname -s)"
UNAME_M="$(uname -m)"

case "$UNAME_S" in
  Linux)
    OS="Linux"
    ;;
  Darwin)
    OS="Darwin"
    ;;
  *)
    fail "Unsupported operating system: $UNAME_S"
    ;;
esac

case "$UNAME_M" in
  x86_64|amd64)
    ARCH="amd64"
    ;;
  arm64|aarch64)
    ARCH="arm64"
    ;;
  *)
    fail "Unsupported CPU architecture: $UNAME_M"
    ;;
esac

ASSET="${BIN_NAME}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${VERSION}/${ASSET}"

if ! command -v tar >/dev/null 2>&1; then
  fail "tar is required to unpack ${ASSET}"
fi

TMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t kubeaid-cli)"
trap 'rm -rf "$TMP_DIR"' EXIT INT TERM

ARCHIVE_PATH="${TMP_DIR}/${ASSET}"

if command -v curl >/dev/null 2>&1; then
  curl -fsSL "$URL" -o "$ARCHIVE_PATH"
elif command -v wget >/dev/null 2>&1; then
  wget -qO "$ARCHIVE_PATH" "$URL"
else
  fail "curl or wget is required to download ${ASSET}"
fi

tar -xzf "$ARCHIVE_PATH" -C "$TMP_DIR"
[ -f "${TMP_DIR}/${BIN_NAME}" ] || fail "Archive did not contain ${BIN_NAME}"

if [ ! -d "$INSTALL_DIR" ]; then
  if [ -w "$(dirname "$INSTALL_DIR")" ]; then
    mkdir -p "$INSTALL_DIR"
  elif command -v sudo >/dev/null 2>&1; then
    sudo mkdir -p "$INSTALL_DIR"
  else
    fail "Cannot create ${INSTALL_DIR}. Run with sufficient permissions."
  fi
fi

TARGET_PATH="${INSTALL_DIR}/${BIN_NAME}"

if [ -w "$INSTALL_DIR" ]; then
  mv "${TMP_DIR}/${BIN_NAME}" "$TARGET_PATH"
  chmod +x "$TARGET_PATH"
elif command -v sudo >/dev/null 2>&1; then
  sudo mv "${TMP_DIR}/${BIN_NAME}" "$TARGET_PATH"
  sudo chmod +x "$TARGET_PATH"
else
  fail "No write access to ${INSTALL_DIR} and sudo is unavailable."
fi

printf "Installed %s %s to %s\n" "$BIN_NAME" "$VERSION" "$TARGET_PATH"
