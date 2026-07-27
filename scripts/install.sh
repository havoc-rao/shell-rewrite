#!/bin/sh
# shr installer — downloads the latest prebuilt binary from GitHub Releases.
# Usage:  curl -fsSL https://raw.githubusercontent.com/havoc-rao/shell-rewrite/main/scripts/install.sh | sh
# Fallback (needs Go):  go install github.com/havoc-rao/shell-rewrite/cmd/shr@latest
set -eu

OWNER="havoc-rao"
REPO="shell-rewrite"
BINARY="shr"

# ---- detect platform ----
case "$(uname -s)" in
  Darwin*)           OS="darwin" ;;
  Linux*)            OS="linux" ;;
  MINGW*|MSYS*|CYGWIN*) OS="windows"; BINARY="shr.exe" ;;
  *) echo "shr: unsupported OS: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  x86_64|amd64)   ARCH="amd64" ;;
  arm64|aarch64)  ARCH="arm64" ;;
  *) echo "shr: unsupported arch: $(uname -m)" >&2; exit 1 ;;
esac

# ---- install dir (fallback to ~/.local/bin if /usr/local/bin not writable) ----
PREFIX="${PREFIX:-/usr/local}"
BINDIR="${BINDIR:-$PREFIX/bin}"
if [ ! -d "$BINDIR" ] || [ ! -w "$BINDIR" ]; then
  BINDIR="$HOME/.local/bin"
fi
mkdir -p "$BINDIR"

# ---- resolve latest release & matching asset ----
API_URL="https://api.github.com/repos/$OWNER/$REPO/releases/latest"
echo "shr: resolving latest release..."
ASSETS=$(curl -fsSL "$API_URL" | sed -n 's/.*"browser_download_url": *"\([^"]*\)".*/\1/p')

URL=$(echo "$ASSETS" | grep -Ei "_${OS}_${ARCH}\.(tar\.gz|zip)$" | head -n1 || true)
if [ -z "$URL" ]; then
  echo "shr: no prebuilt binary for $OS/$ARCH." >&2
  echo "shr: available assets:" >&2
  echo "$ASSETS" | sed 's/^/  /' >&2
  echo "shr: or install via Go:  go install github.com/$OWNER/$REPO@latest" >&2
  exit 1
fi

# ---- download & extract ----
echo "shr: downloading $URL"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
cd "$TMP"
curl -fsSL -o archive "$URL"

case "$URL" in
  *.tar.gz) tar -xzf archive ;;
  *.zip)    unzip -oq archive ;;
esac

# ---- locate & install binary ----
BIN="$(find . -maxdepth 2 -type f \( -name "$BINARY" -o -name "shr" -o -name "shr.exe" \) | head -n1)"
if [ -z "$BIN" ]; then
  echo "shr: binary '$BINARY' not found in archive" >&2
  exit 1
fi
install -m 0755 "$BIN" "$BINDIR/$BINARY"

echo "shr: installed -> $BINDIR/$BINARY"
if ! command -v "$BINARY" >/dev/null 2>&1; then
  echo "shr: NOTE: '$BINDIR' is not in PATH. Add it:"
  echo "  export PATH=\"$BINDIR:\$PATH\""
fi
echo "shr: next step:  eval \"\$(shr init zsh)\"   # or bash"
