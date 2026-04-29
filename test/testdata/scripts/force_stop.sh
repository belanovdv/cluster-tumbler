#!/usr/bin/env bash
set -euo pipefail

NAME="${1:-unknown}"
FILE="/tmp/ct_${NAME}_start.signal"

sleep 2

rm -f "$FILE"

echo "file $FILE removed force"