#!/usr/bin/env bash
# Start the Working Time tool with a local SQLite database
# Usage:
#   ./test.sh            # starts with ./time_tracking.test.db
#   SQLITE_PATH=./my.db ./test.sh   # custom db path
#   ./test.sh --fresh    # delete existing DB before start

set -euo pipefail

# Move to repo root (directory of this script)
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

DB_FILE_DEFAULT="./time_tracking.test.db"
DB_FILE="${SQLITE_PATH:-$DB_FILE_DEFAULT}"

if [[ "${1:-}" == "--fresh" ]]; then
  if [[ -f "$DB_FILE" ]]; then
    echo "Removing existing DB: $DB_FILE"
    rm -f "$DB_FILE"
  fi
fi

export DB_BACKEND=sqlite
export SQLITE_PATH="$DB_FILE"
# Only used for MSSQL migrations; kept here for clarity
unset DB_AUTO_MIGRATE || true

echo "Starting WorkingTime with SQLite…"
echo "  DB_BACKEND = $DB_BACKEND"
echo "  SQLITE_PATH = $SQLITE_PATH"
echo "  Credentials file = $(pwd)/credentials.csv"
echo "App will listen on http://localhost:8083"

# Build flags consistent with build.sh (optional)
export GO111MODULE=on
export GOFLAGS="-buildvcs=false"

exec go run .
