# Backends

iTTSd is Go-only. It does not embed Python code or model inference code.

The recommended backend is an external `qwen3-tts-server` process with `faster-qwen3-tts` and token-level PCM streaming.

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
- Uses CUDA graphs in the backend
- Keeps Qwen backend warm
- Keeps iTTSd as a small Go daemon

External backend project used in the reference setup:

```text
https://github.com/malaiwah/qwen3-tts-server
```

Install helper:

```bash
./scripts/setup-qwen3-fast-backend.sh
```

## Future backend ideas

- Native Rust or Go model backend adapter
- Direct PipeWire output instead of spawning `pw-play`
- Kokoro/Piper HTTP backend adapters
- OpenAI-compatible remote backend adapter
