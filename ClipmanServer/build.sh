#!/usr/bin/env zsh
set -euo pipefail

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
TEMP_ROOT="${CLIPMAN_TEMP_ROOT:-$HOME/Projects/Codex/Temp/clipman}"
MAC_DIST="${CLIPMAN_SERVER_MAC_DIST_DIR:-$TEMP_ROOT/server-mac-dist}"
RELEASE_DIST="${CLIPMAN_SERVER_RELEASE_OUTPUT_DIR:-$TEMP_ROOT/server-release-dist}"
COMBINED_DIST="${CLIPMAN_SERVER_COMBINED_OUTPUT_DIR:-$TEMP_ROOT/server-combined-dist}"
WINDOWS_WRAPPER="${CLIPMAN_SERVER_WINDOWS_EXE:-}"

if [ -z "$WINDOWS_WRAPPER" ] || [ ! -f "$WINDOWS_WRAPPER" ]; then
  echo "Set CLIPMAN_SERVER_WINDOWS_EXE to the Windows clipmanserver wrapper built by Build.ps1." >&2
  exit 2
fi

CLIPMAN_SERVER_MAC_DIST_DIR="$MAC_DIST" \
  zsh "$ROOT/ClipmanServerMac/Scripts/package-release.sh"
CLIPMAN_SERVER_WINDOWS_EXE="$WINDOWS_WRAPPER" \
CLIPMAN_SERVER_MAC_APP="$MAC_DIST/Clipman Server.app" \
CLIPMAN_SERVER_RELEASE_OUTPUT_DIR="$RELEASE_DIST" \
  zsh "$ROOT/ClipmanServerMac/Scripts/package-release-layout.sh"
CLIPMAN_SERVER_WINDOWS_EXE="$WINDOWS_WRAPPER" \
CLIPMAN_SERVER_MAC_APP="$MAC_DIST/Clipman Server.app" \
CLIPMAN_SERVER_COMBINED_OUTPUT_DIR="$COMBINED_DIST" \
  zsh "$ROOT/ClipmanServerMac/Scripts/package-combined-server.sh"

echo "Built native release layout and transition package."
