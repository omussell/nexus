#!/usr/bin/env bash
#
# Run the full Crossref Research Nexus pipeline from scratch.
#
#   1. Clean existing artifacts (SQLite DBs, output dirs, DuckDB, Parquet dirs)
#   2. Build nauvis, croid, fulgora, vulcanus from source
#   3. Run nauvis to decompress Crossref snapshots and record DOIs
#   4. Start croid as an HTTP server
#   5. Run fulgora fetch per-source (ror, retractionwatch)
#   6. Run vulcanus to ingest nauvis + fulgora data and run ROR/retraction
#      matching
#   7. Shut down croid
#
# Every step fails fast: any non-zero exit aborts the script immediately and
# kills the background croid process.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"

NAUVIS_DIR="$REPO_ROOT/systems/nauvis"
CROID_DIR="$REPO_ROOT/systems/croid"
FULGORA_DIR="$REPO_ROOT/systems/fulgora"
VULCANUS_DIR="$REPO_ROOT/systems/vulcanus"

CROID_PORT=9090
CROID_URL="http://localhost:${CROID_PORT}"

CROID_PID=""

cleanup() {
  if [ -n "$CROID_PID" ] && kill -0 "$CROID_PID" 2>/dev/null; then
    kill "$CROID_PID" 2>/dev/null || true
    wait "$CROID_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT

###############################################################################
# 1. Clean existing artifacts
###############################################################################
echo "=== Cleaning existing artifacts ==="

rm -f "$NAUVIS_DIR/nauvis.sqlite3"
rm -rf "$NAUVIS_DIR/out" "$NAUVIS_DIR/parquet"
rm -f "$CROID_DIR/croid.sqlite3"
rm -f "$FULGORA_DIR/fulgora.sqlite3"
rm -rf "$FULGORA_DIR/ror/output" "$FULGORA_DIR/retractionwatch/output"
rm -f "$VULCANUS_DIR/vulcanus.duckdb"

echo "=== Cleaning complete ==="

###############################################################################
# 2. Build binaries
###############################################################################
echo ""
echo "=== Building nauvis ==="
cd "$NAUVIS_DIR" && go build -o nauvis .

echo ""
echo "=== Building croid ==="
cd "$CROID_DIR" && go build -o croid .

echo ""
echo "=== Building fulgora ==="
cd "$FULGORA_DIR" && go build -o fulgora .

echo ""
echo "=== Building vulcanus ==="
cd "$VULCANUS_DIR" && go build -o vulcanus .

echo "=== Build complete ==="

###############################################################################
# 3. Run nauvis: decompress + record DOIs
###############################################################################
echo ""
echo "=== Step 1/4: nauvis (decompress + record DOIs) ==="
cd "$NAUVIS_DIR" && ./nauvis -in data -out out -db nauvis.sqlite3

###############################################################################
# 4. Start croid (background)
###############################################################################
echo ""
echo "=== Step 2/4: croid (HTTP server on port $CROID_PORT) ==="
cd "$CROID_DIR" && ./croid -addr ":${CROID_PORT}" -db croid.sqlite3 &
CROID_PID=$!

for i in $(seq 1 10); do
  if curl -sf "${CROID_URL}/healthz" >/dev/null 2>&1; then
    echo "croid is ready (pid $CROID_PID)"
    break
  fi
  if [ "$i" -eq 10 ]; then
    echo "ERROR: croid failed to start within 10 seconds" >&2
    exit 1
  fi
  sleep 1
done

###############################################################################
# 5. Run fulgora fetch per-source
###############################################################################
echo ""
echo "=== Step 3/4: fulgora fetch (per-source) ==="
cd "$FULGORA_DIR"

echo "--- fulgora fetch ror ---"
./fulgora fetch -croid-url "$CROID_URL" -parquet-dir ror/parquet ror

echo "--- fulgora fetch retractionwatch ---"
./fulgora fetch -croid-url "$CROID_URL" -parquet-dir retractionwatch/parquet retractionwatch

echo "=== Fulgora fetch complete ==="

###############################################################################
# 6. Run vulcanus: ingest + matching
###############################################################################
echo ""
echo "=== Step 4/4: vulcanus (ingest + matching) ==="
cd "$VULCANUS_DIR" && ./vulcanus \
  -nauvis-db "$NAUVIS_DIR/nauvis.sqlite3" \
  -nauvis-dir "$NAUVIS_DIR" \
  -fulgora-db "$FULGORA_DIR/fulgora.sqlite3" \
  -fulgora-dir "$FULGORA_DIR" \
  -match-ror \
  -match-retractions

echo ""
echo "=== Pipeline complete ==="
