#!/usr/bin/env bash
# Lean side of evident: the library builds, the example model exports exactly
# the committed testdata, and every way to fake a proof is rejected.
set -euo pipefail
cd "$(dirname "$0")"

lake build Evident EvidentTest
lake env lean EvidentTest/Export.lean
git diff --exit-code -- ../testdata/gate.json || { echo "testdata/gate.json is stale: commit the export"; exit 1; }

for f in EvidentTest/negative/*.lean; do
  if out=$(lake env lean "$f" 2>&1); then
    echo "FAIL: $f elaborated, but a faked proof must be rejected"; exit 1
  fi
  grep -q "depends on axiom" <<<"$out" || { echo "FAIL: $f failed for another reason:"; echo "$out"; exit 1; }
  echo "rejected as expected: $f"
done
