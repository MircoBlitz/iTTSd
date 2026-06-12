# Backends

iTTSd supports two backend modes.

## Fast streaming backend

Recommended.

Uses an external `qwen3-tts-server` process with `faster-qwen3-tts` and token-level PCM streaming.

iTTSd flag:

```bash
--fast-url http://127.0.0.1:8001
```

Flow:

```text
ittsd → /v1/audio/speech/pcm-stream → raw PCM → pw-play --raw
```

Pros:

- Very low first-audio latency
- No full WAV generation before playback
- Uses CUDA graphs
- Keeps Qwen backend warm

Cons:

- Requires separate Python service
- Tempo control is currently not applied to the raw PCM stream

## Fallback Python worker

If `--fast-url` is empty, iTTSd starts `worker.py` directly and communicates over JSONL stdin/stdout.

Flags:

```bash
--python ~/qwen3-tts-test/.venv/bin/python
--worker ~/dev/ittsd/worker.py
```

Flow:

```text
ittsd → persistent worker.py → WAV chunk → sox tempo → pw-play
```

Pros:

- Simple
- No external HTTP backend required
- Tempo works via `sox`

Cons:

- Much higher first-audio latency
- Generates WAV before playback

## Future backend ideas

- Native Rust `faster-qwen3` backend
- Direct PipeWire playback instead of spawning `pw-play`
- Kokoro/Piper fallback backends
- OpenAI-compatible remote backend adapter
