#!/bin/sh
# shr installer — downloads the latest prebuilt binary from GitHub Releases
# and wires up shell integration.
#
#   curl -fsSL .../install.sh | sh                         # 安装 + 写 rc（重启 shell 生效）
#   eval "$(curl -fsSL .../install.sh | sh)"               # 同上，且当前会话立即生效
#
# Fallback (needs Go):  go install github.com/havoc-rao/shell-rewrite/cmd/shr@latest
#
# 注意：`curl ... | sh` 运行在子 shell，无法直接污染父交互 shell 的函数/PATH。
# 因此所有日志走 stderr；可被父 shell 求值的代码（PATH 守卫 + 加载集成）仅在
# stdout 被 $(...) 捕获时输出，配合 `eval "$(...)"` 即可当前会话即时生效。
set -eu

OWNER="havoc-rao"
REPO="shell-rewrite"
BINARY="shr"

log() { echo "shr: $*" >&2; }

# ---- detect platform ----
case "$(uname -s)" in
  Darwin*)           OS="darwin" ;;
  Linux*)            OS="linux" ;;
  MINGW*|MSYS*|CYGWIN*) OS="windows"; BINARY="shr.exe" ;;
  *) log "unsupported OS: $(uname -s)"; exit 1 ;;
esac

case "$(uname -m)" in
  x86_64|amd64)   ARCH="amd64" ;;
  arm64|aarch64)  ARCH="arm64" ;;
  *) log "unsupported arch: $(uname -m)"; exit 1 ;;
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
log "resolving latest release..."
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
  log "could not resolve latest release (network issue or API rate limit)."
  log "set GITHUB_TOKEN to bypass rate limit, or install via Go:"
  log "  go install github.com/$OWNER/$REPO/cmd/$BINARY@latest"
  exit 1
fi

VERSION="${TAG#v}"  # strip leading 'v'
URL="https://github.com/$OWNER/$REPO/releases/download/$TAG/shr_${VERSION}_${OS}_${ARCH}.${EXT}"

# ---- download & extract ----
log "downloading $URL"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
cd "$TMP"
curl -fsSL -o archive "$URL" || {
  log "download failed for $OS/$ARCH."
  log "browse available assets:  https://github.com/$OWNER/$REPO/releases/tag/$TAG"
  log "or install via Go:  go install github.com/$OWNER/$REPO/cmd/$BINARY@latest"
  exit 1
}

case "$EXT" in
  tar.gz) tar -xzf archive ;;
  zip)    unzip -oq archive ;;
esac

# ---- locate & install binary ----
BIN="$(find . -maxdepth 2 -type f \( -name "$BINARY" -o -name "shr" -o -name "shr.exe" \) | head -n1)"
if [ -z "$BIN" ]; then
  log "binary '$BINARY' not found in archive"
  exit 1
fi
install -m 0755 "$BIN" "$BINDIR/$BINARY"
log "installed -> $BINDIR/$BINARY"

# ---- 接入 shell ----
# 两种用法：
#   curl ... | sh                  stdout 是终端 → 启动交互式 `shr setup` 向导
#   eval "$(curl ... | sh)"        stdout 被 $(...) 捕获 → 非交互写 rc + 即时加载当前会话
SHR_BIN="$BINDIR/$BINARY"

if [ -t 1 ]; then
  # 直接 `| sh`：脚本 stdin 是管道（脚本本体），交互输入需从 /dev/tty 读取。
  if [ -x "$SHR_BIN" ] && [ -r /dev/tty ]; then
    "$SHR_BIN" setup < /dev/tty || \
      "$SHR_BIN" setup --yes --bin-dir "$BINDIR" >&2 || true
  else
    # 无 /dev/tty（极少数环境）：退回非交互应用
    [ -x "$SHR_BIN" ] && "$SHR_BIN" setup --yes --bin-dir "$BINDIR" >&2 || true
    log "restart your shell to activate shr."
  fi
  log "to reconfigure later:  $SHR_BIN setup"
else
  # `eval "$(...)"`：非交互写 rc（幂等），并输出可求值代码让父 shell 当前会话即时加载
  [ -x "$SHR_BIN" ] && "$SHR_BIN" setup --yes --bin-dir "$BINDIR" >&2 || true
  # 运行时幂等地把 BINDIR 加入 PATH，再用绝对路径加载集成（规避 PATH 竞态）
  printf 'case ":$PATH:" in *":%s:"*) ;; *) export PATH="%s:$PATH" ;; esac\n' "$BINDIR" "$BINDIR"
  printf 'eval "$(%s init %s)"\n' "'$SHR_BIN'" "$SH"
fi
