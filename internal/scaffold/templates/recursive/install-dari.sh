#!/usr/bin/env bash
set -euo pipefail

if command -v dari >/dev/null 2>&1; then
  dari --version
  exit 0
fi

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
if [[ "$os" != "linux" ]]; then
  echo "unsupported OS for automatic Dari CLI install: $os" >&2
  exit 1
fi

case "$(uname -m)" in
  x86_64|amd64) arch="x86_64" ;;
  aarch64|arm64) arch="arm64" ;;
  *)
    echo "unsupported architecture for automatic Dari CLI install: $(uname -m)" >&2
    exit 1
    ;;
esac

repo="mupt-ai/dari-cli"
version="$(python3 - <<'PY'
import json, urllib.request
with urllib.request.urlopen('https://api.github.com/repos/mupt-ai/dari-cli/releases/latest', timeout=30) as response:
    payload = json.load(response)
print(payload['tag_name'])
PY
)"
archive_version="${version#v}"
archive="dari_${archive_version}_linux_${arch}.tar.gz"
url="https://github.com/${repo}/releases/download/${version}/${archive}"
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

curl -fsSL "$url" -o "$tmpdir/dari.tar.gz"
tar -xzf "$tmpdir/dari.tar.gz" -C "$tmpdir"

if install -m 0755 "$tmpdir/dari" /usr/local/bin/dari 2>/dev/null; then
  dari --version
  exit 0
fi

mkdir -p "$HOME/.local/bin"
install -m 0755 "$tmpdir/dari" "$HOME/.local/bin/dari"
echo 'Dari CLI installed at $HOME/.local/bin/dari. Add $HOME/.local/bin to PATH if needed.'
"$HOME/.local/bin/dari" --version
