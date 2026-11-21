#!/usr/bin/env bash
set -euo pipefail

# Render assigns the PORT environment variable arbitrarily, so pass arguments to start the app on that port
# Default run, keeping the skill/data paths
: "${PORT:?the PORT environment variable must be set}"
echo "[*] Starting ARTEX Server on port :$PORT"
exec ./artex -addr ":$PORT"
