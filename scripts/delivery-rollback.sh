#!/usr/bin/env bash
# Prepare an explicitly approved rollback commit, or sync a separately published revision and verify it.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
exec ruby -C "$ROOT" "$ROOT/scripts/delivery-release-flow.rb" rollback "$@"
