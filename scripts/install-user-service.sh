#!/usr/bin/env bash
set -euo pipefail
cd "$HOME/dev/ittsd"
mkdir -p bin "$HOME/.config/systemd/user"
go build -o bin/ittsd ./cmd/ittsd
install -m 0644 systemd/ittsd.service "$HOME/.config/systemd/user/ittsd.service"
install -m 0644 systemd/qwen3-fast-tts.service "$HOME/.config/systemd/user/qwen3-fast-tts.service"
systemctl --user daemon-reload

# Disable legacy development service name if present.
systemctl --user disable --now tts-daemon.service >/dev/null 2>&1 || true
echo "Installed user services: qwen3-fast-tts.service, ittsd.service"
echo "Start backend: systemctl --user start qwen3-fast-tts.service"
echo "Start daemon:  systemctl --user start ittsd.service"
echo "Enable: systemctl --user enable qwen3-fast-tts.service ittsd.service"
