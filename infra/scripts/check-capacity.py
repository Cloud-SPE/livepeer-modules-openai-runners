"""Exercise real ASGI handlers without loading models; run in runner images."""
import asyncio
import importlib
import json
import sys
from unittest.mock import patch


async def main():
    module = importlib.import_module(sys.argv[1] + ".app")
    module._semaphore = asyncio.Semaphore(0)
    cases = {
        "openai_tts_runner": [("/v1/audio/speech", {"input": "hello"})],
        "openai_image_generation_runner": [("/v1/images/generations", {"prompt": "a tree"})],
        "rerank_runner": [("/v1/rerank", {"query": "hello", "documents": ["hello"]})],
        "openai_audio_runner": [
            ("/v1/audio/transcriptions", None),
            ("/v1/audio/translations", None),
        ],
    }
    for path, payload in cases[sys.argv[1]]:
        if payload is None:
            body = (b'--test\r\nContent-Disposition: form-data; name="model"\r\n\r\n'
                    b'whisper-large-v3\r\n--test\r\nContent-Disposition: form-data; '
                    b'name="file"; filename="test.wav"\r\nContent-Type: audio/wav\r\n'
                    b'\r\naudio\r\n--test--\r\n')
            content_type = b"multipart/form-data; boundary=test"
        else:
            body = json.dumps(payload).encode()
            content_type = b"application/json"
        messages = []

        async def receive():
            return {"type": "http.request", "body": body, "more_body": False}

        async def send(message):
            messages.append(message)

        scope = {"type": "http", "asgi": {"version": "3.0"}, "http_version": "1.1",
                 "method": "POST", "scheme": "http", "path": path, "raw_path": path.encode(),
                 "query_string": b"", "root_path": "", "server": ("test", 80),
                 "client": ("test", 1), "headers": [(b"content-type", content_type)]}
        # Any attempt to schedule inference after refusing capacity fails the test.
        with patch.object(asyncio.get_running_loop(), "run_in_executor",
                          side_effect=AssertionError("inference started")):
            if payload is None:
                with patch.object(module, "_decode_audio", return_value=[0] * 16000):
                    await module.app(scope, receive, send)
            else:
                await module.app(scope, receive, send)
        start = next(m for m in messages if m["type"] == "http.response.start")
        headers = dict(start["headers"])
        result = b"".join(m.get("body", b"") for m in messages if m["type"] == "http.response.body")
        assert start["status"] == 429, (path, start, result)
        assert json.loads(result) == {"error": "capacity_reached"}, result
        assert headers[b"retry-after"] == b"5", headers
        assert b"x-livepeer-work-units" not in headers, headers
        assert b"livepeer-work-units" not in headers, headers
        print(f"PASS {path}: capacity refusal before inference")


asyncio.run(main())
