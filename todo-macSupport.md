# TODO: macOS / Apple Silicon support for iTTSd

This document is for the next assistant session running on the Mac.

## Goal

Add macOS support to the existing `MircoBlitz/iTTSd` repo, not a new repo.

Core idea:

```text
iTTSd core stays Go-only
  → HTTP input
  → queue
  → backend adapter
  → local playback
```

Linux/NVIDIA path already works:

```text
iTTSd → qwen3-fast-tts :8001 /v1/audio/speech/pcm-stream → raw PCM → pw-play
```

macOS target path:

```text
iTTSd → mlx-tts-server :8000 /v1/audio/speech → WAV/MP3 → afplay
```

Later ideal path:

```text
iTTSd → MLX backend streaming PCM → native CoreAudio playback
```

## Current repository

Repo:

```text
git@github.com:MircoBlitz/iTTSd.git
```

Linux dev path:

```text
~/dev/ittsd
```

Important existing files:

```text
cmd/ittsd/main.go
README.md
docs/api.md
docs/backends.md
docs/install.md
docs/performance.md
docs/systemd.md
docs/llm-prompt.md
scripts/install-user-service.sh
scripts/setup-qwen3-fast-backend.sh
scripts/latency-test.sh
systemd/ittsd.service
systemd/qwen3-fast-tts.service
```

## Current Linux performance baseline

With external `faster-qwen3-tts` backend on RTX 4070 Ti:

```text
accepted_to_first_chunk_ms:    ~156 ms
accepted_to_first_playback_ms: ~156 ms
```

## Current tested Qwen voice preferences

```text
top: Vivian
ok: Aiden, Serena, Uncle_Fu, Ono_Anna
no: Ryan, Dylan, Eric, Sohee
```

Default voice should stay:

```text
Vivian
```

## Important design decision

The repo should remain **Go-only for iTTSd itself**.

Do not reintroduce embedded Python worker code into iTTSd.

External model servers are allowed and should be managed separately:

- Linux: `malaiwah/qwen3-tts-server` with `faster-qwen3-tts`
- macOS: likely `realAllenSong/mlx-tts-server` or another MLX HTTP server

## macOS research findings

Most relevant backend:

```text
https://github.com/realAllenSong/mlx-tts-server
```

It is OpenAI-compatible and supports Apple Silicon / MLX.

Example command from research:

```bash
mlx-tts serve mlx-community/Qwen3-TTS-12Hz-0.6B-CustomVoice-4bit --port 8000
```

Example request:

```bash
curl -X POST http://localhost:8000/v1/audio/speech \
  -H "Content-Type: application/json" \
  -d '{"model":"tts-1","input":"Hello, Apple Silicon!","voice":"ryan","response_format":"wav"}' \
  --output speech.wav
```

Useful MLX model candidates:

```text
mlx-community/Qwen3-TTS-12Hz-0.6B-CustomVoice-4bit
mlx-community/Qwen3-TTS-12Hz-0.6B-CustomVoice-8bit
mlx-community/Qwen3-TTS-12Hz-0.6B-Base-4bit
mlx-community/Qwen3-TTS-12Hz-0.6B-Base-8bit
```

Start with:

```text
mlx-community/Qwen3-TTS-12Hz-0.6B-CustomVoice-4bit
```

Other relevant Mac/MLX projects found:

```text
https://github.com/kapi2800/qwen3-tts-apple-silicon
https://github.com/suckerfish/qwen3-tts-mlx
https://github.com/NickBouwhuis/QwenTTS
https://github.com/louiscoetzee/mlx-tts-studio
https://github.com/AtomGradient/swift-qwen3-tts
```

## Required iTTSd changes for macOS

### 1. Backend abstraction

Current iTTSd assumes `--fast-url` and Qwen PCM stream endpoint.

Add backend mode flags, e.g.:

```bash
--backend qwen3-fast-pcm        # current Linux mode
--backend openai-audio-file     # macOS MLX MVP
--backend-url http://127.0.0.1:8000
```

For backward compatibility:

```bash
--fast-url http://127.0.0.1:8001
```

can map to:

```text
--backend qwen3-fast-pcm --backend-url http://127.0.0.1:8001
```

### 2. Playback abstraction

Current playback is Linux-only:

```bash
pw-play --raw --rate 24000 --channels 1 --format s16 -
```

Add player mode flags:

```bash
--player pipewire   # Linux raw PCM
--player afplay     # macOS file playback
--player command    # generic command template later
```

MVP macOS path:

```text
backend returns WAV file bytes → write temp .wav → afplay temp.wav
```

Possible future:

```text
backend streams PCM → native CoreAudio player in Go
```

### 3. macOS LaunchAgent

Add:

```text
launchd/com.mircoblitz.ittsd.plist
launchd/com.mircoblitz.qwen3-mlx-tts.plist
```

or:

```text
macos/launchd/*.plist
```

Commands:

```bash
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.mircoblitz.ittsd.plist
launchctl enable gui/$(id -u)/com.mircoblitz.ittsd
launchctl kickstart -k gui/$(id -u)/com.mircoblitz.ittsd
```

### 4. macOS setup script

Add:

```text
scripts/setup-macos-mlx-backend.sh
scripts/install-macos-launchagent.sh
```

Expected setup script rough shape:

```bash
brew install go uv
mkdir -p ~/dev
# install/clone mlx-tts-server or use uv tool if package supports it
# install model/backend deps
# install launch agents
```

Do not assume Homebrew path is Intel or ARM; handle `/opt/homebrew` and `/usr/local` if needed.

### 5. Docs

Add:

```text
docs/macos.md
```

Include:

- Apple Silicon requirement
- MLX backend setup
- LaunchAgent install/start/stop
- `afplay` playback path
- latency test instructions
- differences from Linux/NVIDIA path

Update README with a macOS section.

## API compatibility target

Keep iTTSd API stable:

```http
POST /speak
GET /job/{id}
GET /status
GET /voices
GET /health
```

LLM prompt should stay valid:

```text
docs/llm-prompt.md
```

## macOS testing checklist

1. Clone repo:

```bash
git clone git@github.com:MircoBlitz/iTTSd.git ~/dev/ittsd
cd ~/dev/ittsd
git checkout -b macos-mlx
```

2. Build Go binary:

```bash
go build -o bin/ittsd ./cmd/ittsd
```

3. Start MLX backend manually.

4. Test backend alone:

```bash
curl -X POST http://localhost:8000/v1/audio/speech \
  -H "Content-Type: application/json" \
  -d '{"model":"tts-1","input":"Hello from MLX.","voice":"vivian","response_format":"wav"}' \
  --output /tmp/mlx-test.wav

afplay /tmp/mlx-test.wav
```

5. Start iTTSd manually with macOS flags, for example:

```bash
./bin/ittsd \
  --addr 127.0.0.1:8765 \
  --backend openai-audio-file \
  --backend-url http://127.0.0.1:8000 \
  --player afplay \
  --voice Vivian \
  --lang auto
```

6. Test iTTSd:

```bash
curl -s http://127.0.0.1:8765/speak \
  -H 'Content-Type: application/json' \
  -d '{"text":"Hello from iTTSd on macOS.","lang":"en","voice":"Vivian"}'
```

7. Run latency test:

```bash
./scripts/latency-test.sh
```

8. Install LaunchAgent and confirm after reboot/login.

## Important caveats

- `afplay` is file-based, so first-audio latency will probably be higher than Linux PCM streaming.
- `mlx-tts-server` may use different voice names or model behavior. Verify `vivian` spelling.
- Some MLX Qwen implementations may return WAV only, not streaming PCM.
- If streaming is unavailable, keep queue correctness first; optimize latency later.

## Commit/push guidance

Use same repo, branch first:

```bash
git checkout -b macos-mlx
```

After testing:

```bash
git add .
git commit -m "Add macOS MLX backend support"
git push -u origin macos-mlx
```
