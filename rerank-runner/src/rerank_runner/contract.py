"""The runner contract for rerank-runner (runner-contract.md).

Served at GET /.well-known/livepeer-runner. Rerank is not an OpenAI
endpoint, so the capability takes the product domain (`text:rerank`) and
the identity lives under the plain `model` key, not `openai.model`. Units
are documents: the runner emits X-Livepeer-Work-Units = len(documents)
on every successful response and declares the response-header extractor.

Pure module: no torch, no FastAPI.
"""

from __future__ import annotations

CONTRACT_PATH = "/.well-known/livepeer-runner"
PROTOCOL = "paid-job/v1"
PAID_JOB_VERSION = "1.0.15"
WORK_UNITS_HEADER = "X-Livepeer-Work-Units"
DEFAULT_CAPABILITY = "text:rerank"
INVOKE_PATH = "/v1/rerank"


def default_model_alias(model_id: str) -> str:
    """`zeroentropy/zerank-2` → `zerank-2`: the name a caller uses, not the HF path."""
    return (model_id or "").rsplit("/", 1)[-1].strip()


def build_contract(
    capability: str,
    *,
    model_alias: str,
    model_id: str,
    provider: str,
    max_documents: int,
) -> dict:
    return {
        "capability_id": (capability or DEFAULT_CAPABILITY).strip(),
        "protocol": PROTOCOL,
        "transports": ["unary"],
        "work_unit": {
            "name": "documents",
            "extractor": {"type": "response-header", "header": WORK_UNITS_HEADER},
        },
        "paths": {"invoke": INVOKE_PATH},
        "readiness": {"type": "http-status", "path": "/healthz"},
        "identity": {"model": model_alias, "provider": provider},
        "schema_versions": {PROTOCOL: PAID_JOB_VERSION},
        "x-backend-model": model_id,
        "x-max-documents": int(max_documents),
    }
