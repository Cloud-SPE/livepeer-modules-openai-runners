"""The runner contract for openai-tts-runner (runner-contract.md).

Served at GET /.well-known/livepeer-runner. Units are input characters,
counted by the broker from the request with a request-formula extractor
(Unicode code points of `input`), so the runner itself does not count.

Pure module: no torch, no FastAPI.
"""

from __future__ import annotations

CONTRACT_PATH = "/.well-known/livepeer-runner"
PROTOCOL = "paid-job/v1"
PAID_JOB_VERSION = "1.0.15"
DEFAULT_CAPABILITY = "openai:audio-speech"
INVOKE_PATH = "/v1/audio/speech"


def build_contract(
    capability: str,
    *,
    model_alias: str,
    model_id: str,
    provider: str,
    output_formats: list[str],
    default_voice: str,
    voices: list[str],
    voice_aliases: dict[str, str],
) -> dict:
    return {
        "capability_id": (capability or DEFAULT_CAPABILITY).strip(),
        "protocol": PROTOCOL,
        "transports": ["unary"],
        "work_unit": {
            "name": "input_chars",
            "extractor": {
                "type": "request-formula",
                "expression": "chars",
                "text_fields": {"chars": "$.input"},
                "default": 0,
            },
        },
        "paths": {"invoke": INVOKE_PATH},
        "readiness": {"type": "http-status", "path": "/healthz"},
        "identity": {"openai.model": model_alias, "provider": provider},
        "schema_versions": {PROTOCOL: PAID_JOB_VERSION},
        "x-backend-model": model_id,
        "x-formats": {"output": sorted(output_formats)},
        "x-default-voice": default_voice,
        "x-voices": {"native": list(voices), "aliases": dict(voice_aliases)},
    }
