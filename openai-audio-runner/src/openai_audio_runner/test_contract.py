import json
import unittest

from .contract import (
    CAP_TRANSCRIPTIONS,
    CAP_TRANSLATIONS,
    build_contract,
    select_capabilities,
)

AGENT_FIELDS = {
    "capability_id", "protocol", "transports", "descriptor_schemas", "work_unit", "paths",
    "readiness", "identity", "schema_versions", "metering", "heartbeat",
    "session_params_schema", "requirements",
}

FACTS = dict(
    model_alias="whisper-large-v3",
    model_id="openai/whisper-large-v3",
    provider="transformers",
    input_formats=["mp3", "wav"],
    output_formats={"vtt", "json", "text"},
)


def assert_relayable(tc: unittest.TestCase, entry: dict) -> None:
    for key in entry:
        tc.assertTrue(key in AGENT_FIELDS or key.startswith("x-"), f"unknown key {key!r}")
    for key in ("local_id", "devices", "draining"):
        tc.assertNotIn(key, entry)
    for key in ("capability_id", "protocol", "paths", "work_unit", "readiness"):
        tc.assertIn(key, entry)
    json.dumps(entry)  # must serialise


class ContractTest(unittest.TestCase):
    def test_default_is_two_entries(self):
        doc = build_contract(None, **FACTS)
        self.assertIsInstance(doc, list)
        self.assertEqual([e["capability_id"] for e in doc], [CAP_TRANSCRIPTIONS, CAP_TRANSLATIONS])
        self.assertEqual([e["paths"]["invoke"] for e in doc],
                         ["/v1/audio/transcriptions", "/v1/audio/translations"])
        for entry in doc:
            assert_relayable(self, entry)
            self.assertEqual(entry["protocol"], "paid-job/v1")
            self.assertEqual(entry["transports"], ["multipart"])
            self.assertEqual(entry["work_unit"], {
                "name": "audio_seconds",
                "extractor": {"type": "response-header", "header": "X-Livepeer-Work-Units"},
            })
            self.assertEqual(entry["readiness"], {"type": "http-status", "path": "/healthz"})
            self.assertEqual(entry["identity"], {"openai.model": "whisper-large-v3", "provider": "transformers"})
            self.assertEqual(entry["schema_versions"], {"paid-job/v1": "1.0.15"})
            self.assertEqual(entry["x-backend-model"], "openai/whisper-large-v3")
            self.assertEqual(entry["x-formats"], {"input": ["mp3", "wav"], "output": ["json", "text", "vtt"]})

    def test_capability_name_restricts_to_one_object(self):
        doc = build_contract(CAP_TRANSLATIONS, **FACTS)
        self.assertIsInstance(doc, dict)
        self.assertEqual(doc["capability_id"], CAP_TRANSLATIONS)
        self.assertEqual(doc["paths"]["invoke"], "/v1/audio/translations")

    def test_unknown_capability_name_is_a_startup_error(self):
        with self.assertRaises(ValueError):
            select_capabilities("openai-audio-transcriptions")  # the old hyphen form
        self.assertEqual(select_capabilities("  "), [CAP_TRANSCRIPTIONS, CAP_TRANSLATIONS])


if __name__ == "__main__":
    unittest.main()
