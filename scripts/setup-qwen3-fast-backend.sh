#!/usr/bin/env bash
set -euo pipefail

ROOT="$HOME/dev/qwen3-tts-server-fast"
UV="$HOME/.local/bin/uv"

if ! command -v git >/dev/null; then
  echo "git is required" >&2
  exit 1
fi
if [[ ! -x "$UV" ]]; then
  echo "uv is required at $UV" >&2
  echo "Install: curl -LsSf https://astral.sh/uv/install.sh | sh" >&2
  exit 1
fi
mkdir -p "$HOME/dev"
if [[ ! -d "$ROOT/.git" ]]; then
  git clone https://github.com/malaiwah/qwen3-tts-server.git "$ROOT"
fi
cd "$ROOT"
"$UV" venv --python 3.12 .venv
"$UV" pip install torch --index-url https://download.pytorch.org/whl/cu128
"$UV" pip install --force-reinstall torchaudio --index-url https://download.pytorch.org/whl/cu128
"$UV" pip install -e . faster-qwen3-tts

echo "Installed qwen3 fast backend in $ROOT"
echo "Start it with: systemctl --user start qwen3-fast-tts.service"
