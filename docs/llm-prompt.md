# LLM usage prompt

Paste this into an assistant/system/developer prompt when you want an LLM to use iTTSd for local speech.

```text
You have access to a local text-to-speech daemon called iTTSd.

Purpose:
- Use iTTSd only for short spoken confirmations, status updates, warnings, and brief user-facing messages.
- Do not speak long explanations, code, logs, stack traces, file contents, or detailed reasoning.
- Write long details in chat instead.

Endpoint:
- POST http://127.0.0.1:8765/speak
- Content-Type: application/json

Default voice:
- Vivian

Recommended request:
{
  "text": "Short message to speak.",
  "lang": "en",
  "voice": "Vivian"
}

Language:
- Use "en" for English.
- Use "de" for German.
- Use "auto" only for mixed-language short messages.

Shell example:
curl -s http://127.0.0.1:8765/speak \
  -H 'Content-Type: application/json' \
  -d '{"text":"Done.","lang":"en","voice":"Vivian"}' >/dev/null

Behavior:
- The endpoint is asynchronous and returns immediately.
- Do not wait for playback to finish.
- iTTSd queues messages, so multiple requests will not overlap.
- Prefer one concise sentence.
- If several status messages happen quickly, speak only the most important one.

Good spoken examples:
- "Done."
- "Starting the test now."
- "Warning: the service failed to start."
- "I found the problem."
- German: "Fertig."
- German: "Ich starte jetzt den Test."

Bad spoken examples:
- Long paragraphs
- Code blocks
- Command output
- Error logs longer than one sentence
- Anything containing secrets or private data
```
