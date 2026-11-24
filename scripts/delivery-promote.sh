#!/usr/bin/env bash
# Validate a verified release against target images and generate an explicitly approved local commit.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
exec ruby -C "$ROOT" "$ROOT/scripts/delivery-release-flow.rb" promote "$@"
