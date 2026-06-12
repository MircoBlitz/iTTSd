#!/usr/bin/env python3
"""Persistent Qwen3-TTS JSONL worker.

Protocol:
  stdin:  one JSON object per line
  stdout: one JSON object per line

Request:
  {"id":"...","text":"...","model":"custom-0.6b","lang":"de","speaker":"Vivian","out":"/tmp/x.wav"}
Response:
  {"id":"...","ok":true,"out":"/tmp/x.wav","sr":24000}
"""
import json
import sys
from pathlib import Path

import torch
import soundfile as sf
from qwen_tts import Qwen3TTSModel

MODEL_ALIASES = {
    "custom-0.6b": "Qwen/Qwen3-TTS-12Hz-0.6B-CustomVoice",
    "custom-1.7b": "Qwen/Qwen3-TTS-12Hz-1.7B-CustomVoice",
    "design-1.7b": "Qwen/Qwen3-TTS-12Hz-1.7B-VoiceDesign",
    "clone-0.6b": "Qwen/Qwen3-TTS-12Hz-0.6B-Base",
    "clone-1.7b": "Qwen/Qwen3-TTS-12Hz-1.7B-Base",
}

LANG_ALIASES = {
    "en": "English",
    "de": "German",
    "auto": "Auto",
    "english": "English",
    "german": "German",
}

DTYPES = {
    "bfloat16": torch.bfloat16,
    "float16": torch.float16,
    "float32": torch.float32,
}

_models = {}


def respond(obj):
    print(json.dumps(obj, ensure_ascii=False), flush=True)


def load_model(model_name, device, dtype_name):
    model_id = MODEL_ALIASES.get(model_name, model_name)
    key = (model_id, device, dtype_name)
    if key not in _models:
        print(f"[worker] loading {model_id} on {device} dtype={dtype_name}", file=sys.stderr, flush=True)
        _models[key] = Qwen3TTSModel.from_pretrained(
            model_id,
            device_map=device,
            dtype=DTYPES.get(dtype_name, torch.bfloat16),
        )
        print(f"[worker] ready {model_id}", file=sys.stderr, flush=True)
    return _models[key], model_id


def handle(req):
    rid = req.get("id", "")
    text = (req.get("text") or "").strip()
    if not text:
        raise ValueError("text is empty")

    model_name = req.get("model") or "custom-0.6b"
    device = req.get("device") or "cuda:0"
    dtype_name = req.get("dtype") or "bfloat16"
    language = LANG_ALIASES.get((req.get("lang") or "auto").lower(), req.get("lang") or "Auto")
    speaker = req.get("speaker") or "Vivian"
    instruct = req.get("instruct") or ""
    out = Path(req.get("out") or f"/tmp/tts-{rid}.wav").expanduser()
    out.parent.mkdir(parents=True, exist_ok=True)

    model, model_id = load_model(model_name, device, dtype_name)
    lower_model = model_id.lower()

    if "customvoice" in lower_model:
        kwargs = dict(text=text, language=language, speaker=speaker)
        if instruct:
            kwargs["instruct"] = instruct
        wavs, sr = model.generate_custom_voice(**kwargs)
    elif "voicedesign" in lower_model:
        wavs, sr = model.generate_voice_design(
            text=text,
            language=language,
            instruct=instruct or "A clear, warm assistant voice.",
        )
    else:
        ref_audio = req.get("ref_audio")
        if not ref_audio:
            raise ValueError("clone/base model requires ref_audio")
        wavs, sr = model.generate_voice_clone(
            text=text,
            language=language,
            ref_audio=ref_audio,
            ref_text=req.get("ref_text"),
        )

    sf.write(out, wavs[0], sr)
    return {"id": rid, "ok": True, "out": str(out), "sr": sr}


def main():
    print("[worker] started", file=sys.stderr, flush=True)
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            req = json.loads(line)
            if req.get("cmd") == "ping":
                respond({"id": req.get("id", ""), "ok": True, "pong": True})
                continue
            resp = handle(req)
            respond(resp)
        except Exception as e:
            rid = ""
            try:
                rid = req.get("id", "")  # type: ignore[name-defined]
            except Exception:
                pass
            print(f"[worker] error: {e}", file=sys.stderr, flush=True)
            respond({"id": rid, "ok": False, "error": str(e)})


if __name__ == "__main__":
    main()
