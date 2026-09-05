"""The runner contract for openai-image-generation-runner (runner-contract.md).

Served at GET /.well-known/livepeer-runner. Units are images, counted by
the broker from the request's `n` (default 1) with a request-formula
extractor. The identity is MODEL_ID as configured: the catalog matches
image templates on the HF id (e.g. black-forest-labs/FLUX.1-dev).

Pure module: no torch, no FastAPI.
"""

from __future__ import annotations

CONTRACT_PATH = "/.well-known/livepeer-runner"
PROTOCOL = "paid-job/v1"
PAID_JOB_VERSION = "1.0.15"
DEFAULT_CAPABILITY = "openai:images-generations"
INVOKE_PATH = "/v1/images/generations"


def build_contract(
    capability: str,
    *,
    model_id: str,
    provider: str,
    default_width: int,
    default_height: int,
    response_formats: list[str],
) -> dict:
    return {
        "capability_id": (capability or DEFAULT_CAPABILITY).strip(),
        "protocol": PROTOCOL,
        "transports": ["unary"],
        "work_unit": {
            "name": "images",
            "extractor": {
                "type": "request-formula",
                "expression": "n",
                "fields": {"n": "$.n"},
                "default": 1,
            },
        },
        "paths": {"invoke": INVOKE_PATH},
        "readiness": {"type": "http-status", "path": "/healthz"},
        "identity": {"openai.model": model_id, "provider": provider},
        "schema_versions": {PROTOCOL: PAID_JOB_VERSION},
        "x-default-size": f"{default_width}x{default_height}",
        "x-formats": {"output": sorted(response_formats)},
    }
