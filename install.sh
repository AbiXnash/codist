#!/bin/sh
# Installs Codist on macOS or Linux, then checks what it needs.
#
#   curl -fsSL https://raw.githubusercontent.com/abxengg/codist/remaster/install.sh | sh
#
# Settings, all optional:
#   CODIST_VERSION   a release tag such as v0.1.0 (default: the latest release)
#   CODIST_BIN_DIR   where to put the binary (default: ~/.local/bin)
#   CODIST_FROM_SOURCE=1  build from source with Go instead of downloading
set -eu

REPO="abxengg/codist"
BRANCH="remaster"
BIN_DIR="${CODIST_BIN_DIR:-$HOME/.local/bin}"
VERSION="${CODIST_VERSION:-latest}"

say()  { printf '%s\n' "$*"; }
ok()   { printf '  ok       %s\n' "$*"; }
miss() { printf '  missing  %s\n' "$*"; }
fail() { printf 'codist install: %s\n' "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

# Which build fits this machine.
case "$(uname -s)" in
  Darwin) os=macos ;;
  Linux)  os=linux ;;
  *) fail "this script is for macOS and Linux; on Windows run install.ps1 in PowerShell." ;;
esac
case "$(uname -m)" in
  arm64|aarch64) arch=arm64 ;;
  x86_64|amd64)  arch=x64 ;;
  *) fail "no build for $(uname -m); build from source with CODIST_FROM_SOURCE=1." ;;
esac
asset="codist-$os-$arch"

download() { # url file
  if have curl; then curl -fsSL "$1" -o "$2"
  elif have wget; then wget -qO "$2" "$1"
  else fail "needs curl or wget to download."
  fi
}

from_source() {
  have go || fail "no release to download and Go is not installed. Install Go from https://go.dev/dl, or wait for a release."
  have git || fail "building from source needs git."
  tmp=$(mktemp -d)
  say "Building Codist from source (branch $BRANCH)..."
  git clone -q --depth 1 -b "$BRANCH" "https://github.com/$REPO.git" "$tmp/codist"
  (cd "$tmp/codist" && CGO_ENABLED=0 go build -tags 'grammar_subset grammar_subset_java' -o "$tmp/codist-bin" .)
  mv "$tmp/codist-bin" "$BIN_DIR/codist"
  rm -rf "$tmp"
}

mkdir -p "$BIN_DIR"
if [ "${CODIST_FROM_SOURCE:-}" = "1" ]; then
  from_source
else
  if [ "$VERSION" = "latest" ]; then
    url="https://github.com/$REPO/releases/latest/download/$asset"
  else
    url="https://github.com/$REPO/releases/download/$VERSION/$asset"
  fi
  say "Downloading $asset ($VERSION)..."
  if download "$url" "$BIN_DIR/codist.tmp" 2>/dev/null; then
    mv "$BIN_DIR/codist.tmp" "$BIN_DIR/codist"
  else
    rm -f "$BIN_DIR/codist.tmp"
    say "No release found for $asset."
    from_source
  fi
fi
chmod +x "$BIN_DIR/codist"
# macOS marks downloaded files; a binary that is not notarised would otherwise be blocked on first run.
[ "$os" = macos ] && xattr -d com.apple.quarantine "$BIN_DIR/codist" 2>/dev/null || true
say "Installed $BIN_DIR/codist"
say ""

say "Checking what Codist needs:"
if have git; then ok "git $(git --version | awk '{print $3}')"; else miss "git: install it (macOS: xcode-select --install; Linux: your package manager)"; fi
if have java; then ok "java (optional; for checks that run code)"; else say "  -        java not found (optional; only for checks that run code)"; fi
model_url="${CODIST_MODEL_URL:-http://localhost:11434}"
if (have curl && curl -fsS -m 3 "$model_url/api/tags" >/dev/null 2>&1); then
  ok "model server at $model_url"
else
  say "  -        no model server at $model_url (optional; Codist still runs its code checks)"
  say "           to add one: install Ollama from https://ollama.com, then: ollama pull qwen2.5-coder:7b"
fi
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) say ""; say "Add $BIN_DIR to your PATH, for example:"; say "  echo 'export PATH=\"$BIN_DIR:\$PATH\"' >> ~/.zshrc   # or ~/.bashrc" ;;
esac
say ""
say "Next: cd into a Java repository and run  codist"
say "      codist doctor  shows the same checks again."
