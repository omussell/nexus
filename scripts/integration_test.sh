#!/usr/bin/env bash
#
# End-to-end integration test for the Crossref Research Nexus pipeline:
#
#   1. Build nauvis and croid from source.
#   2. Run nauvis to record the DOIs of the Crossref snapshot dataset.
#   3. For every recorded DOI, ask croid to mint (or look up) a COID, storing
#      the result in the coid SQLite database via the same path the coid HTTP
#      server uses (POST /coid).
#   4. Confirm every DOI that nauvis recorded has a corresponding row in the
#      coid database.
#
# Artifacts (built binaries and SQLite DBs) live under build/ and are ignored
# by git.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"

NAUVIS_DIR="$REPO_ROOT/systems/nauvis"
NAUVIS_DATA_DIR="$NAUVIS_DIR/data"
CROID_DIR="$REPO_ROOT/systems/croid"

WORK_DIR="$REPO_ROOT/build/integration"
SRC_DIR="$WORK_DIR/src"
NAU_DB="$WORK_DIR/nauvis.sqlite3"
CROID_DB="$WORK_DIR/coid.sqlite3"
NAUVIS_BIN="$WORK_DIR/nauvis"
CROID_BIN="$WORK_DIR/coid"

# Reset the working directory.
rm -rf "$WORK_DIR"
mkdir -p "$SRC_DIR"

cleanup() {
  local exit_code=$?
  rm -rf "$WORK_DIR"
  exit "$exit_code"
}
trap cleanup EXIT

echo "=== Integration Test: nauvis -> coid ==="

# --- Step 1: Build coid ---
echo "--- Building coid ---"
cd "$CROID_DIR"
go build -o "$CROID_BIN" .

# --- Step 2: Build nauvis ---
echo "--- Building nauvis ---"
cd "$NAUVIS_DIR"
go build -o "$NAUVIS_BIN" .

# --- Step 3: Copy one source file and record its DOIs with nauvis ---
echo "--- Copying one source file and running nauvis ---"
cp "$NAUVIS_DIR/data/0.json.gz" "$SRC_DIR/0.json.gz"
"$NAUVIS_BIN" -in "$SRC_DIR" -out "$WORK_DIR/out" -db "$NAU_DB"

# --- Step 4: Mint a Coid for every DOI nauvis recorded, via the coid CLI ---
echo "--- Minting Coids from recorded DOIs ---"
DOIS="$(sqlite3 "$NAU_DB" 'SELECT doi FROM nauvis ORDER BY doi;')"
NUM_DOIS="$(printf '%s\n' "$DOIS" | grep -c . || true)"

if [ "$NUM_DOIS" -eq 0 ]; then
  echo "❌ No DOIs found in the nauvis database; nothing to verify."
  exit 1
fi
echo "nauvis recorded $NUM_DOIS DOIs."

# Mint one coid per DOI, deduped against whatever is already in the coid DB.
while IFS= read -r doi; do
  [ -z "$doi" ] && continue
  "$CROID_BIN" --generate --input \
    "{\"cro_type\":\"DOI\",\"cro_value\":\"$doi\",\"system\":\"nauvis\"}" \
    --db "$CROID_DB" >/dev/null
done <<< "$DOIS"
echo "coid CLI processed $NUM_DOIS DOIs."

# --- Step 5: Verify each DOI has a Coid in the coid database ---
echo "--- Verifying Coids in the coid database ---"
EXPECTED=$NUM_DOIS
ACTUAL=$(sqlite3 "$CROID_DB" \
  "SELECT COUNT(*) FROM cro_ids WHERE cro_type='DOI';" 2>/dev/null || echo 0)

echo "Expected $EXPECTED DOI rows; found $ACTUAL in the coid database."

if [ "$EXPECTED" -eq "$ACTUAL" ]; then
  echo "✅ Integration test passed: every DOI recorded by nauvis has a matching Coid in the coid database."
  exit 0
else
  echo "❌ Integration test failed: expected $EXPECTED DOI rows, found $ACTUAL."
  exit 1
fi
