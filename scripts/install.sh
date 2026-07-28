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

# ---- archive extension by OS ----
case "$OS" in
  windows) EXT="zip" ;;
  *)       EXT="tar.gz" ;;
esac

# ---- resolve latest release tag ----
# Primary: follow the github.com redirect (releases/latest → releases/tag/vX.Y.Z).
# This endpoint is NOT subject to api.github.com rate limits (60 req/h/IP).
echo "shr: resolving latest release..."
TAG=$(curl -fsSLI -o /dev/null -w '%{url_effective}' \
  "https://github.com/$OWNER/$REPO/releases/latest" 2>/dev/null \
  | sed 's|.*/tag/||' || true)

# Fallback: query the API (optionally authenticated via GITHUB_TOKEN / GH_TOKEN
# to bypass the 60 req/h unauthenticated limit on shared cloud IPs).
if [ -z "$TAG" ] || [ "$TAG" = "https://github.com/$OWNER/$REPO/releases/latest" ]; then
  API_URL="https://api.github.com/repos/$OWNER/$REPO/releases/latest"
  TOKEN="${GITHUB_TOKEN:-}"
  [ -z "$TOKEN" ] && TOKEN="${GH_TOKEN:-}"
  if [ -n "$TOKEN" ]; then
    TAG=$(curl -fsSL -H "Authorization: Bearer $TOKEN" "$API_URL" 2>/dev/null \
      | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' || true)
  else
    TAG=$(curl -fsSL "$API_URL" 2>/dev/null \
      | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' || true)
  fi
fi

if [ -z "$TAG" ]; then
  echo "shr: could not resolve latest release (network issue or API rate limit)." >&2
  echo "shr: set GITHUB_TOKEN to bypass rate limit, or install via Go:" >&2
  echo "  go install github.com/$OWNER/$REPO/cmd/$BINARY@latest" >&2
  exit 1
fi

VERSION="${TAG#v}"  # strip leading 'v'
URL="https://github.com/$OWNER/$REPO/releases/download/$TAG/shr_${VERSION}_${OS}_${ARCH}.${EXT}"

# ---- download & extract ----
echo "shr: downloading $URL"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
cd "$TMP"
curl -fsSL -o archive "$URL" || {
  echo "shr: download failed for $OS/$ARCH." >&2
  echo "shr: browse available assets:  https://github.com/$OWNER/$REPO/releases/tag/$TAG" >&2
  echo "shr: or install via Go:  go install github.com/$OWNER/$REPO/cmd/$BINARY@latest" >&2
  exit 1
}

case "$EXT" in
  tar.gz) tar -xzf archive ;;
  zip)    unzip -oq archive ;;
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
