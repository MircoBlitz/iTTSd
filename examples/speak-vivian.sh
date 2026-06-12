#!/usr/bin/env bash
set -euo pipefail
curl -s http://127.0.0.1:8765/speak \
  -H 'Content-Type: application/json' \
  -d '{"text":"Hello from Vivian. This is iTTSd speaking locally with queued streaming playback.","lang":"en","voice":"Vivian"}'
echo
