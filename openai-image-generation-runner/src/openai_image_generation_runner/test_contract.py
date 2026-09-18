import json
import unittest

from .contract import DEFAULT_CAPABILITY, build_contract

AGENT_FIELDS = {
    "capability_id", "protocol", "transports", "descriptor_schemas", "work_unit", "paths",
    "readiness", "identity", "schema_versions", "metering", "heartbeat",
    "session_params_schema", "requirements",
}


class ContractTest(unittest.TestCase):
    def test_shape(self):
        entry = build_contract(
            "",
            model_id="black-forest-labs/FLUX.1-dev",
            provider="diffusers",
            default_width=1024,
            default_height=768,
            response_formats=["b64_json"],
        )
        for key in entry:
            self.assertTrue(key in AGENT_FIELDS or key.startswith("x-"), f"unknown key {key!r}")
        json.dumps(entry)
        self.assertEqual(entry["capability_id"], DEFAULT_CAPABILITY)
        self.assertEqual(entry["capability_id"], "openai:images-generations")
        self.assertEqual(entry["transports"], ["unary"])
        self.assertEqual(entry["work_unit"], {
            "name": "images",
            "extractor": {"type": "request-formula", "expression": "n", "fields": {"n": "$.n"}, "default": 1},
        })
        self.assertEqual(entry["paths"], {"invoke": "/v1/images/generations"})
        self.assertEqual(entry["readiness"], {"type": "http-status", "path": "/healthz"})
        self.assertEqual(entry["identity"], {"openai.model": "black-forest-labs/FLUX.1-dev", "provider": "diffusers"})
        self.assertEqual(entry["schema_versions"], {"paid-job/v1": "1.0.15"})
        self.assertEqual(entry["x-default-size"], "1024x768")

    def test_capability_override(self):
        entry = build_contract("vision:images", model_id="m", provider="p", default_width=1, default_height=1, response_formats=[])
        self.assertEqual(entry["capability_id"], "vision:images")


if __name__ == "__main__":
    unittest.main()
