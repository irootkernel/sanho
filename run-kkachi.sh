#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export PATH="/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"

if ! command -v git >/dev/null 2>&1; then
  echo "Required command not found: git" >&2
  exit 1
fi

DAEMON_BINARY="$SCRIPT_DIR/bin/sanhod"
if [ ! -x "$DAEMON_BINARY" ]; then
  echo "Daemon binary not found or not executable: $DAEMON_BINARY" >&2
  exit 1
fi

cd "$SCRIPT_DIR"
exec "$DAEMON_BINARY"
