"""The runner contract for openai-audio-runner.

Served at GET /.well-known/livepeer-runner (livepeer-network-protocol
runner-contract.md). This container serves two OpenAI endpoints from one
Whisper load, so by default it returns a JSON array of two capability
entries (runner-contract.md 1.1.0); CAPABILITY_NAME restricts it to one.

Pure module: no torch, no FastAPI, so it can be unit-tested anywhere.
"""

from __future__ import annotations

CONTRACT_PATH = "/.well-known/livepeer-runner"
PROTOCOL = "paid-job/v1"
PAID_JOB_VERSION = "1.0.15"
WORK_UNITS_HEADER = "X-Livepeer-Work-Units"

CAP_TRANSCRIPTIONS = "openai:audio-transcriptions"
CAP_TRANSLATIONS = "openai:audio-translations"
INVOKE_PATHS = {
    CAP_TRANSCRIPTIONS: "/v1/audio/transcriptions",
    CAP_TRANSLATIONS: "/v1/audio/translations",
}


def select_capabilities(capability_name: str | None) -> list[str]:
    """CAPABILITY_NAME unset/empty → both entries; one of the two ids → that
    entry only (the pool-host shape); anything else is a startup error."""
    name = (capability_name or "").strip()
    if not name:
        return [CAP_TRANSCRIPTIONS, CAP_TRANSLATIONS]
    if name in INVOKE_PATHS:
        return [name]
    raise ValueError(
        f"CAPABILITY_NAME={name!r} is not served by this image; "
        f"use one of {sorted(INVOKE_PATHS)} or leave it unset to serve both"
    )


def build_entry(
    capability: str,
    *,
    model_alias: str,
    model_id: str,
    provider: str,
    input_formats: list[str],
    output_formats: list[str],
) -> dict:
    return {
        "capability_id": capability,
        "protocol": PROTOCOL,
        "transports": ["multipart"],
        "work_unit": {
            "name": "audio_seconds",
            "extractor": {"type": "response-header", "header": WORK_UNITS_HEADER},
        },
        "paths": {"invoke": INVOKE_PATHS[capability]},
        "readiness": {"type": "http-status", "path": "/healthz"},
        "identity": {"openai.model": model_alias, "provider": provider},
        "schema_versions": {PROTOCOL: PAID_JOB_VERSION},
        "x-backend-model": model_id,
        "x-formats": {"input": list(input_formats), "output": sorted(output_formats)},
    }


def build_contract(capability_name: str | None, **facts) -> dict | list[dict]:
    entries = [build_entry(cap, **facts) for cap in select_capabilities(capability_name)]
    return entries[0] if len(entries) == 1 else entries
