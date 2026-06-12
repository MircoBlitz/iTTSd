# iTTSd

**iTTSd** — *instant Text-To-Speech daemon* — is a local, LLM-friendly TTS router written in Go.

It accepts non-blocking HTTP speech requests, queues them so voices never overlap, and plays audio locally through PipeWire. For low latency it can use a streaming Qwen3 backend (`faster-qwen3-tts`) that starts playback from raw PCM chunks instead of waiting for full WAV generation.

## Current performance on the author's RTX 4070 Ti

With the optional `qwen3-fast-tts` backend:

```text
accepted_to_first_chunk_ms:    ~156 ms
accepted_to_first_playback_ms: ~156 ms
```

The slower built-in Python fallback remains available, but the fast backend is recommended.

## Features

- Go single-binary daemon: `ittsd`
- Local HTTP API for LLMs and scripts
- Fire-and-forget `/speak` endpoint
- FIFO queue: multiple LLM messages do **not** speak over each other
- Token-level PCM streaming via `faster-qwen3-tts`
- Fallback persistent Python/Qwen worker
- PipeWire playback via `pw-play`
- Systemd user services
- Latency test script
- Tested defaults for Qwen3-TTS CustomVoice:
  - model: `custom-0.6b` / fast backend `Qwen3-TTS-12Hz-1.7B-Base`
  - voice: `Vivian`
  - tempo: `1.15` for fallback mode

## Architecture

Recommended fast path:

```text
LLM / curl
  → ittsd :8765
  → queued job
  → qwen3-fast-tts :8001 /v1/audio/speech/pcm-stream
  → raw 24 kHz mono s16 PCM
  → pw-play --raw
```

Fallback path:

```text
LLM / curl
  → ittsd :8765
  → queued job
  → persistent Python qwen-tts worker
  → WAV chunks
  → optional sox tempo
  → pw-play
```

## Requirements

Base daemon:

- Linux
- Go 1.25+
- PipeWire tools: `pw-play`
- `sox` for fallback tempo processing

Fast Qwen backend:

- NVIDIA GPU with CUDA support
- Python 3.12 environment
- `torch` CUDA wheels
- `faster-qwen3-tts`
- `qwen3-tts-server` cloned separately

This project intentionally does **not** vendor the external Qwen backend.

## Install quickstart

### 1. Clone this repo

```bash
git clone https://github.com/YOURNAME/ittsd.git ~/dev/ittsd
cd ~/dev/ittsd
```

### 2. Build

```bash
go build -o bin/ittsd ./cmd/ittsd
```

### 3. Install user services

```bash
./scripts/install-user-service.sh
```

### 4. Install the fast Qwen backend

```bash
./scripts/setup-qwen3-fast-backend.sh
```

This clones `malaiwah/qwen3-tts-server` into:

```text
~/dev/qwen3-tts-server-fast
```

and installs the Python dependencies into its `.venv`.

### 5. Start services

```bash
systemctl --user start qwen3-fast-tts.service
systemctl --user start ittsd.service
```

Enable them at login:

```bash
systemctl --user enable qwen3-fast-tts.service ittsd.service
```

Optional, if you want user services to run without an active graphical login:

```bash
loginctl enable-linger "$USER"
```

## Verify

```bash
curl http://127.0.0.1:8001/health
curl http://127.0.0.1:8765/health
```

Expected `ittsd` response:

```json
{"ok": true}
```

## Speak

```bash
curl -s http://127.0.0.1:8765/speak \
  -H 'Content-Type: application/json' \
  -d '{"text":"Hello from Vivian. This is iTTSd speaking locally.","lang":"en","voice":"Vivian"}'
```

Or:

```bash
./scripts/speak.sh "Hello from Vivian. This is iTTSd speaking locally."
```

## Latency test

Run this in a real terminal:

```bash
./scripts/latency-test.sh
```

Press a key when you hear the first audio. The script prints perceived latency and daemon-side latency metrics.

## HTTP API

### `POST /speak`

Queue speech and return immediately.

Request:

```json
{
  "text": "Hallo. Hello.",
  "lang": "auto",
  "voice": "Vivian",
  "model": "custom-0.6b",
  "tempo": 1.15,
  "instruct": "optional style instruction"
}
```

Response:

```json
{"job_id":"abc123","status":"queued"}
```

### `GET /job/{id}`

Returns job state and timings.

### `GET /status`

Returns current generation/playback state, queue lengths, and known jobs.

### `GET /voices`

Returns the locally tested voice ratings.

### `GET /health`

Health check.

## Voice notes

Tested Qwen preset voices:

- top: `Vivian`
- ok: `Aiden`, `Serena`, `Uncle_Fu`, `Ono_Anna`
- no: `Ryan`, `Dylan`, `Eric`, `Sohee`

## Development

```bash
gofmt -w cmd/ittsd/main.go
go build -o bin/ittsd ./cmd/ittsd
```

Restart the service after rebuild:

```bash
systemctl --user restart ittsd.service
```

## Project status

Early prototype, but already useful locally. The API and config flags may still change.

## License

MIT. See `LICENSE`.
