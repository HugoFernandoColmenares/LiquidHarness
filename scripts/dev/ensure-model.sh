#!/bin/sh
# Nightly helper: fail fast when the cached model is missing. The self-hosted runner owns the
# download; this script never fetches weights during a run.
set -eu

if [ -z "${LIQUIDHARNESS_MODEL_PATH:-}" ]; then
	echo "ensure-model: set LIQUIDHARNESS_MODEL_PATH to the cached GGUF file" >&2
	exit 1
fi

if [ ! -f "$LIQUIDHARNESS_MODEL_PATH" ]; then
	echo "ensure-model: model not found: $LIQUIDHARNESS_MODEL_PATH" >&2
	exit 1
fi

echo "ensure-model: using $LIQUIDHARNESS_MODEL_PATH"
