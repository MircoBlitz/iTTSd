# API

Default base URL:

```text
http://127.0.0.1:8765
```

## POST /speak

Queue speech asynchronously. The request returns immediately; playback happens in FIFO order.

```bash
curl -s http://127.0.0.1:8765/speak \
  -H 'Content-Type: application/json' \
  -d '{"text":"Hello from Vivian.","lang":"en","voice":"Vivian"}'
```

Request fields:

| Field | Type | Default | Notes |
|---|---:|---|---|
| `text` | string | required | Text to speak |
| `lang` | string | `auto` | `en`, `de`, `auto`, etc. |
| `voice` | string | `Vivian` | Qwen preset voice |
| `speaker` | string | same as `voice` | Alias for fallback/backend use |
| `model` | string | `custom-0.6b` | Fallback model alias |
| `tempo` | number | `1.15` | Used by fallback WAV path; fast PCM path ignores tempo currently |
| `instruct` | string | empty | Optional style instruction |

Response:

```json
{"job_id":"abc123","status":"queued"}
```

## GET /job/{id}

Returns full job information including daemon-side timings.

```bash
curl -s http://127.0.0.1:8765/job/abc123 | python3 -m json.tool
```

Latency fields:

```json
{
  "latencies": {
    "accepted_to_first_chunk_ms": 156,
    "accepted_to_first_playback_ms": 156
  }
}
```

## GET /status

Returns daemon status, queue lengths, and known jobs.

```bash
curl -s http://127.0.0.1:8765/status | python3 -m json.tool
```

## GET /voices

Returns tested voice ratings.

```bash
curl -s http://127.0.0.1:8765/voices | python3 -m json.tool
```

## GET /health

```bash
curl http://127.0.0.1:8765/health
```
