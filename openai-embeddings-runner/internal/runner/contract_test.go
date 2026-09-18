package runner

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
)

// Mirrors the closed key set the pool member agent enforces.
var agentContractFields = map[string]bool{
	"capability_id": true, "protocol": true, "transports": true, "descriptor_schemas": true,
	"work_unit": true, "paths": true, "readiness": true, "identity": true, "schema_versions": true,
	"metering": true, "heartbeat": true, "session_params_schema": true, "requirements": true,
}

func assertRelayable(t *testing.T, entry map[string]any) {
	t.Helper()
	for k := range entry {
		if !agentContractFields[k] && !(len(k) >= 2 && k[:2] == "x-") {
			t.Fatalf("contract key %q is neither a runner-attach field nor x-*", k)
		}
	}
	for _, k := range []string{"local_id", "devices", "draining"} {
		if _, present := entry[k]; present {
			t.Fatalf("contract must not carry agent-owned key %q", k)
		}
	}
}

func roundTrip(t *testing.T, doc any) any {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestBuildContract_SingleEntry(t *testing.T) {
	doc := roundTrip(t, buildContract("openai:embeddings", []string{"Qwen3-Embedding-8B"}, "total_tokens",
		contractConfig{upstreamKind: "vllm", embeddingDimensions: 4096, maxInputTokens: 32768, poolingMode: "last", backendModel: "Qwen/Qwen3-Embedding-8B"}))
	entry, ok := doc.(map[string]any)
	if !ok {
		t.Fatalf("want one object, got %T", doc)
	}
	assertRelayable(t, entry)
	if entry["capability_id"] != "openai:embeddings" || entry["protocol"] != "paid-job/v1" {
		t.Fatalf("capability/protocol = %v/%v", entry["capability_id"], entry["protocol"])
	}
	if !reflect.DeepEqual(entry["transports"], []any{"unary"}) {
		t.Fatalf("transports = %v", entry["transports"])
	}
	wu := entry["work_unit"].(map[string]any)
	if wu["name"] != "tokens" || wu["extractor"].(map[string]any)["type"] != "openai-usage" {
		t.Fatalf("work_unit = %v", wu)
	}
	if entry["paths"].(map[string]any)["invoke"] != "/v1/embeddings" {
		t.Fatalf("paths = %v", entry["paths"])
	}
	rd := entry["readiness"].(map[string]any)
	if rd["type"] != "http-openai-model-ready" || rd["path"] != "/v1/models" || rd["config"].(map[string]any)["model"] != "Qwen3-Embedding-8B" {
		t.Fatalf("readiness = %v", rd)
	}
	id := entry["identity"].(map[string]any)
	if id["openai.model"] != "Qwen3-Embedding-8B" || id["provider"] != "vllm" {
		t.Fatalf("identity = %v", id)
	}
	if entry["schema_versions"].(map[string]any)["paid-job/v1"] != "1.0.15" {
		t.Fatalf("schema_versions = %v", entry["schema_versions"])
	}
	if entry["x-embedding-dimensions"] != float64(4096) || entry["x-max-input-tokens"] != float64(32768) ||
		entry["x-pooling-mode"] != "last" || entry["x-backend-model"] != "Qwen/Qwen3-Embedding-8B" {
		t.Fatalf("extensions wrong: %v", entry)
	}
}

func TestBuildContract_OmitsUnsetExtensions(t *testing.T) {
	entry := roundTrip(t, buildContract("openai:embeddings", []string{"m"}, "total_tokens", contractConfig{})).(map[string]any)
	for _, k := range []string{"x-backend-model", "x-embedding-dimensions", "x-max-input-tokens", "x-pooling-mode"} {
		if _, present := entry[k]; present {
			t.Fatalf("%s should be omitted when unset", k)
		}
	}
	if _, present := entry["identity"].(map[string]any)["provider"]; present {
		t.Fatal("provider should be omitted when upstream kind is unset")
	}
}

func TestBuildContract_ServedModelNameAndArray(t *testing.T) {
	if doc := roundTrip(t, buildContract("openai:embeddings", []string{"a", "b"}, "prompt_tokens", contractConfig{servedModelName: "served"})); doc.(map[string]any)["identity"].(map[string]any)["openai.model"] != "served" {
		t.Fatalf("SERVED_MODEL_NAME should win: %v", doc)
	}
	entries, ok := roundTrip(t, buildContract("openai:embeddings", []string{"a", "b"}, "prompt_tokens", contractConfig{upstreamKind: "ollama"})).([]any)
	if !ok || len(entries) != 2 {
		t.Fatalf("want array of 2 entries")
	}
	for _, e := range entries {
		assertRelayable(t, e.(map[string]any))
		if e.(map[string]any)["work_unit"].(map[string]any)["extractor"].(map[string]any)["field"] != "prompt_tokens" {
			t.Fatalf("usage field not propagated")
		}
	}
}

func TestHandleContractAndModels(t *testing.T) {
	var models atomic.Value
	rec := httptest.NewRecorder()
	handleContract(rec, httptest.NewRequest(http.MethodGet, contractPath, nil), "openai:embeddings", "total_tokens", contractConfig{}, &models)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("before discovery: %d", rec.Code)
	}
	models.Store([]string{"bge"})
	rec = httptest.NewRecorder()
	handleContract(rec, httptest.NewRequest(http.MethodGet, contractPath, nil), "openai:embeddings", "total_tokens", contractConfig{upstreamKind: "vllm"}, &models)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status/content-type = %d/%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	var entry map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry["identity"].(map[string]any)["openai.model"] != "bge" {
		t.Fatalf("identity = %v", entry["identity"])
	}

	rec = httptest.NewRecorder()
	handleModels(rec, httptest.NewRequest(http.MethodGet, modelsPath, nil), contractConfig{upstreamKind: "vllm"}, &models)
	var list struct {
		Object string `json:"object"`
		Data   []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || list.Object != "list" || len(list.Data) != 1 || list.Data[0].ID != "bge" {
		t.Fatalf("/v1/models = %s (%v)", rec.Body.String(), err)
	}
}
