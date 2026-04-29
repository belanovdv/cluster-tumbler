#!/usr/bin/env bash
set -euo pipefail

NAME="${1:-unknown}"
FILE="/tmp/ct_${NAME}_start.signal"

sleep 1

if [[ ! -f "$FILE" ]]; then
  echo "file $FILE removed"
  exit 0
else
  echo "file $FILE still exists"
  exit 1
fi