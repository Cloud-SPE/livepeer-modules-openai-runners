import json
import unittest

from .contract import DEFAULT_CAPABILITY, build_contract, default_model_alias

AGENT_FIELDS = {
    "capability_id", "protocol", "transports", "descriptor_schemas", "work_unit", "paths",
    "readiness", "identity", "schema_versions", "metering", "heartbeat",
    "session_params_schema", "requirements",
}


class ContractTest(unittest.TestCase):
    def test_shape(self):
        entry = build_contract(
            "",
            model_alias="zerank-2",
            model_id="zeroentropy/zerank-2",
            provider="sentence-transformers",
            max_documents=1000,
        )
        for key in entry:
            self.assertTrue(key in AGENT_FIELDS or key.startswith("x-"), f"unknown key {key!r}")
        json.dumps(entry)
        self.assertEqual(entry["capability_id"], DEFAULT_CAPABILITY)
        self.assertEqual(entry["capability_id"], "text:rerank")
        self.assertEqual(entry["transports"], ["unary"])
        self.assertEqual(entry["work_unit"], {
            "name": "documents",
            "extractor": {"type": "response-header", "header": "X-Livepeer-Work-Units"},
        })
        self.assertEqual(entry["paths"], {"invoke": "/v1/rerank"})
        self.assertEqual(entry["readiness"], {"type": "http-status", "path": "/healthz"})
        # Plain `model`, never `openai.model`: rerank is not an OpenAI endpoint.
        self.assertEqual(entry["identity"], {"model": "zerank-2", "provider": "sentence-transformers"})
        self.assertNotIn("openai.model", entry["identity"])
        self.assertEqual(entry["schema_versions"], {"paid-job/v1": "1.0.15"})
        self.assertEqual(entry["x-backend-model"], "zeroentropy/zerank-2")
        self.assertEqual(entry["x-max-documents"], 1000)

    def test_default_alias_strips_hf_namespace(self):
        self.assertEqual(default_model_alias("zeroentropy/zerank-2"), "zerank-2")
        self.assertEqual(default_model_alias("zerank-2"), "zerank-2")


if __name__ == "__main__":
    unittest.main()
