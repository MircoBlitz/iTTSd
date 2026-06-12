#!/usr/bin/env bash
set -euo pipefail

BASE="${TTS_BASE:-http://127.0.0.1:8765}"
TEXT="${*:-Vivian wants to tell you about the concert yesterday evening. The hall was packed, the lights were warm and golden, and the first song started with a quiet guitar before the whole room suddenly came alive. By the final encore, everyone was singing together, and Vivian walked home still hearing the drums in her head.}"

json_escape() {
  python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))'
}

TEXT_JSON=$(printf '%s' "$TEXT" | json_escape)
PAYLOAD=$(cat <<JSON
{"text":$TEXT_JSON,"lang":"en","voice":"Vivian","model":"custom-0.6b","tempo":1.15}
JSON
)

printf 'Submitting TTS job...\n'
START=$(date +%s.%N)
RESP=$(curl -sS "$BASE/speak" -H 'Content-Type: application/json' -d "$PAYLOAD")
SUBMITTED=$(date +%s.%N)
JOB_ID=$(python3 -c 'import json,sys; print(json.load(sys.stdin).get("job_id",""))' <<<"$RESP")
printf 'Response: %s\n' "$RESP"
printf 'Job: %s\n' "$JOB_ID"
printf '\nPress SPACE/any key when you hear the first audio.\n'
printf 'Waiting... '

if ! IFS= read -r -s -n 1 _ < /dev/tty 2>/dev/null; then
  IFS= read -r -s -n 1 _
fi
END=$(date +%s.%N)

python3 - <<PY
start=float("$START")
submitted=float("$SUBMITTED")
end=float("$END")
print(f"\nSubmit HTTP time: {submitted-start:.3f}s")
print(f"Perceived first-audio latency: {end-start:.3f}s")
print(f"After HTTP response: {end-submitted:.3f}s")
PY

if [[ -n "$JOB_ID" ]]; then
  printf '\nDaemon-side job metrics:\n'
  curl -sS "$BASE/job/$JOB_ID" | python3 -m json.tool | sed -n '/"latencies"/,/}/p'
fi
