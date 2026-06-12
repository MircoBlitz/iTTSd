# Installation

This guide installs iTTSd as systemd user services.

## Prerequisites

```bash
sudo dnf install -y golang sox git
```

`pw-play` must be available through PipeWire tools. On most modern desktop Linux installations it already is.

Check:

```bash
command -v go
command -v pw-play
command -v sox
```

## Build and install iTTSd

```bash
git clone https://github.com/MircoBlitz/iTTSd.git ~/dev/ittsd
cd ~/dev/ittsd
./scripts/install-user-service.sh
```

This builds:

```text
~/dev/ittsd/bin/ittsd
```

and installs:

```text
~/.config/systemd/user/ittsd.service
~/.config/systemd/user/qwen3-fast-tts.service
```

## Install fast Qwen backend

```bash
cd ~/dev/ittsd
./scripts/setup-qwen3-fast-backend.sh
```

The backend is cloned to:

```text
~/dev/qwen3-tts-server-fast
```

## Start

```bash
systemctl --user start qwen3-fast-tts.service
systemctl --user start ittsd.service
```

## Enable autostart

```bash
systemctl --user enable qwen3-fast-tts.service ittsd.service
```

Optional linger:

```bash
loginctl enable-linger "$USER"
```

## Verify

```bash
systemctl --user status qwen3-fast-tts.service
systemctl --user status ittsd.service
curl http://127.0.0.1:8001/health
curl http://127.0.0.1:8765/health
```

## Logs

```bash
journalctl --user -u qwen3-fast-tts.service -f
journalctl --user -u ittsd.service -f
```
