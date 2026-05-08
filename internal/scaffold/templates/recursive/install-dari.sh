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
version="${DARI_CLI_VERSION:-}"
if [[ -z "$version" ]]; then
  latest_url="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/${repo}/releases/latest")"
  version="${latest_url##*/}"
fi
if [[ "$version" != v* ]]; then
  version="v${version}"
fi
if [[ ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "could not resolve Dari CLI release version: $version" >&2
  exit 1
fi
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

home_dir="${HOME:-/tmp}"
mkdir -p "$home_dir/.local/bin"
install -m 0755 "$tmpdir/dari" "$home_dir/.local/bin/dari"
echo "Dari CLI installed at $home_dir/.local/bin/dari. Add $home_dir/.local/bin to PATH if needed."
"$home_dir/.local/bin/dari" --version
