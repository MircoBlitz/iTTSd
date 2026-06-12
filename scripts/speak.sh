#!/usr/bin/env bash
set -euo pipefail
TEXT="${*:-}"
if [[ -z "$TEXT" ]]; then TEXT=$(cat); fi
JSON=$(python3 -c 'import json,sys; print(json.dumps({"text": sys.argv[1]}))' "$TEXT")
curl -sS http://127.0.0.1:8765/speak -H 'Content-Type: application/json' -d "$JSON"
