# Performance

## Test setup

Initial development machine:

- GPU: NVIDIA RTX 4070 Ti, 12 GB VRAM
- Driver: 595.x
- OS: Fedora/Nobara Linux
- Audio: PipeWire

## Fallback Python worker

Baseline with persistent Python worker but full WAV chunks:

```text
accepted_to_first_playback_ms: ~2730 ms
```

Short single-word chunks can be around 1 second, but normal first sentences are slower.

## Fast streaming backend

With `faster-qwen3-tts` token-level PCM streaming:

```text
accepted_to_first_chunk_ms:    ~156 ms
accepted_to_first_playback_ms: ~156 ms
```

This is the recommended configuration.

## Run perceived latency test

```bash
./scripts/latency-test.sh
```

Press any key when you hear the first audio.

## Interpretation

- `Submit HTTP time`: curl/request overhead
- `Perceived first-audio latency`: your measured human-perceived time
- `accepted_to_first_playback_ms`: daemon-side time until first PCM/audio write begins

Human keypress latency is expected to be higher than daemon-side latency.
