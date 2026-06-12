# iTTSd Spec

## Goal

A local, LLM-friendly TTS service with:

- non-blocking HTTP input
- asynchronous job queue
- no overlapping playback
- warm TTS backend
- streaming first-audio latency
- systemd user service startup

## Defaults

- Voice: `Vivian`
- Language: `auto`
- Listen: `127.0.0.1:8765`
- Fast backend: `http://127.0.0.1:8001`

## HTTP API

### POST /speak

Fire-and-forget queued speech.

Request:

```json
{
  "text": "Hallo. Hello.",
  "lang": "auto",
  "voice": "Vivian",
  "model": "custom-0.6b",
  "tempo": 1.15
}
```

Response:

```json
{"job_id":"...","status":"queued"}
```

### GET /job/{id}

Returns timings and status.

### GET /status

Returns current generation, current playback, queue lengths, and known jobs.

### GET /voices

Returns tested voice ratings.

### GET /health

Returns `{ "ok": true }`.

## Backend strategy

### Fast backend

When `--fast-url` is set, iTTSd uses the external Qwen3 streaming server:

```text
iTTSd → /v1/audio/speech/pcm-stream → raw PCM → pw-play --raw
```

This is the recommended path and can produce sub-200ms daemon-side first playback on suitable NVIDIA GPUs.

## Future work

- Direct PipeWire output instead of spawning `pw-play`
- Better cancellation / barge-in
- Config file support
- OpenAI-compatible `/v1/audio/speech` endpoint on iTTSd itself
- Additional backend adapters
