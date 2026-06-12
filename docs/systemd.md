# systemd

iTTSd is intended to run as a user service.

Installed units:

```text
~/.config/systemd/user/qwen3-fast-tts.service
~/.config/systemd/user/ittsd.service
```

## Start

```bash
systemctl --user start qwen3-fast-tts.service
systemctl --user start ittsd.service
```

## Stop

```bash
systemctl --user stop ittsd.service
systemctl --user stop qwen3-fast-tts.service
```

## Restart

```bash
systemctl --user restart qwen3-fast-tts.service
systemctl --user restart ittsd.service
```

## Enable on login

```bash
systemctl --user enable qwen3-fast-tts.service ittsd.service
```

## Logs

```bash
journalctl --user -u qwen3-fast-tts.service -f
journalctl --user -u ittsd.service -f
```

## Linger

If you want the user services to keep running after logout:

```bash
loginctl enable-linger "$USER"
```
