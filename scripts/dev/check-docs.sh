#!/bin/sh
# Validate links, code fences and encodings in the documentation.
set -eu
cd "$(dirname "$0")/../.."
exec go run ./scripts/dev/checkdocs "$@"
