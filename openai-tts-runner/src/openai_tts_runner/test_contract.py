import json
import unittest

from .contract import DEFAULT_CAPABILITY, build_contract

AGENT_FIELDS = {
    "capability_id", "protocol", "transports", "descriptor_schemas", "work_unit", "paths",
    "readiness", "identity", "schema_versions", "metering", "heartbeat",
    "session_params_schema", "requirements",
}


class ContractTest(unittest.TestCase):
    def build(self, capability=""):
        return build_contract(
            capability,
            model_alias="kokoro",
            model_id="hexgrad/Kokoro-82M",
            provider="kokoro",
            output_formats=["wav", "mp3"],
            default_voice="af_bella",
            voices=["af_bella", "am_michael"],
            voice_aliases={"alloy": "af_bella"},
        )

    def test_shape(self):
        entry = self.build()
        for key in entry:
            self.assertTrue(key in AGENT_FIELDS or key.startswith("x-"), f"unknown key {key!r}")
        json.dumps(entry)
        self.assertEqual(entry["capability_id"], DEFAULT_CAPABILITY)
        self.assertEqual(entry["capability_id"], "openai:audio-speech")
        self.assertEqual(entry["transports"], ["unary"])
        self.assertEqual(entry["work_unit"]["name"], "input_chars")
        self.assertEqual(entry["work_unit"]["extractor"], {
            "type": "request-formula", "expression": "chars",
            "text_fields": {"chars": "$.input"}, "default": 0,
        })
        self.assertEqual(entry["paths"], {"invoke": "/v1/audio/speech"})
        self.assertEqual(entry["readiness"], {"type": "http-status", "path": "/healthz"})
        self.assertEqual(entry["identity"], {"openai.model": "kokoro", "provider": "kokoro"})
        self.assertEqual(entry["schema_versions"], {"paid-job/v1": "1.0.15"})
        self.assertEqual(entry["x-formats"], {"output": ["mp3", "wav"]})
        self.assertEqual(entry["x-default-voice"], "af_bella")
        self.assertEqual(entry["x-voices"]["aliases"], {"alloy": "af_bella"})

    def test_capability_override(self):
        self.assertEqual(self.build(" custom:speech ")["capability_id"], "custom:speech")


if __name__ == "__main__":
    unittest.main()
