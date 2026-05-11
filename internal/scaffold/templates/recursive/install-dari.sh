#!/usr/bin/env bash
set -euo pipefail

if command -v dari >/dev/null 2>&1; then
  dari --version
  exit 0
fi

# Set DARI_CLI_VERSION (for example, v1.2.3) to pin the CLI version.
installer_url="${DARI_INSTALLER_URL:-https://raw.githubusercontent.com/mupt-ai/dari-cli/main/install.sh}"

curl -fsSL "$installer_url" | bash
