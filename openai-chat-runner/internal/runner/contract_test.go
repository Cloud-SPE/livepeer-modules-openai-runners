package runner

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
)

// The contract must decode into exactly the runner-owned fields of
// runner-attach §3.2 (plus x-*). This mirrors the closed key set the
// pool member agent enforces in internal/attach/contract.go.
var agentContractFields = map[string]bool{
	"capability_id": true, "protocol": true, "transports": true, "descriptor_schemas": true,
	"work_unit": true, "paths": true, "readiness": true, "identity": true, "schema_versions": true,
	"metering": true, "heartbeat": true, "session_params_schema": true, "requirements": true,
}

func assertRelayable(t *testing.T, entry map[string]any) {
	t.Helper()
	for k := range entry {
		if !agentContractFields[k] && len(k) < 2 || (!agentContractFields[k] && k[:2] != "x-") {
			t.Fatalf("contract key %q is neither a runner-attach field nor x-*", k)
		}
	}
	for _, k := range []string{"local_id", "devices", "draining"} {
		if _, present := entry[k]; present {
			t.Fatalf("contract must not carry agent-owned key %q", k)
		}
	}
	for _, k := range []string{"capability_id", "protocol", "paths", "work_unit", "readiness"} {
		if _, present := entry[k]; !present {
			t.Fatalf("contract missing required key %q", k)
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

func TestBuildContract_SingleModelIsOneEntry(t *testing.T) {
	doc := roundTrip(t, buildContract("openai:chat-completions", []string{"Qwen3.6-27B"}, "total_tokens",
		contractConfig{upstreamKind: "vllm", quantization: "nvfp4", contextLength: 32768}))
	entry, ok := doc.(map[string]any)
	if !ok {
		t.Fatalf("single model should serialise as one object, got %T", doc)
	}
	assertRelayable(t, entry)

	if entry["capability_id"] != "openai:chat-completions" || entry["protocol"] != "paid-job/v1" {
		t.Fatalf("capability/protocol = %v/%v", entry["capability_id"], entry["protocol"])
	}
	if !reflect.DeepEqual(entry["transports"], []any{"unary", "stream"}) {
		t.Fatalf("transports = %v", entry["transports"])
	}
	wu := entry["work_unit"].(map[string]any)
	ex := wu["extractor"].(map[string]any)
	if wu["name"] != "tokens" || ex["type"] != "openai-usage" || ex["field"] != "total_tokens" {
		t.Fatalf("work_unit = %v", wu)
	}
	if entry["paths"].(map[string]any)["invoke"] != "/v1/chat/completions" {
		t.Fatalf("paths = %v", entry["paths"])
	}
	rd := entry["readiness"].(map[string]any)
	if rd["type"] != "http-openai-model-ready" || rd["path"] != "/v1/models" ||
		rd["config"].(map[string]any)["model"] != "Qwen3.6-27B" {
		t.Fatalf("readiness = %v", rd)
	}
	id := entry["identity"].(map[string]any)
	if id["openai.model"] != "Qwen3.6-27B" || id["provider"] != "vllm" {
		t.Fatalf("identity = %v", id)
	}
	if entry["schema_versions"].(map[string]any)["paid-job/v1"] != "1.0.15" {
		t.Fatalf("schema_versions = %v", entry["schema_versions"])
	}
	if entry["x-quantization"] != "nvfp4" || entry["x-context-length"] != float64(32768) {
		t.Fatalf("extensions = %v / %v", entry["x-quantization"], entry["x-context-length"])
	}
	for _, absent := range []string{"x-backend-model", "x-reasoning-parser", "x-tool-call-parser"} {
		if _, present := entry[absent]; present {
			t.Fatalf("%s should be omitted when unset", absent)
		}
	}
}

func TestBuildContract_ServedModelNameWinsOverDiscovery(t *testing.T) {
	doc := roundTrip(t, buildContract("openai:chat-completions", []string{"a", "b"}, "total_tokens",
		contractConfig{servedModelName: "served"}))
	entry, ok := doc.(map[string]any)
	if !ok {
		t.Fatalf("SERVED_MODEL_NAME should collapse to one entry, got %T", doc)
	}
	if entry["identity"].(map[string]any)["openai.model"] != "served" {
		t.Fatalf("identity = %v", entry["identity"])
	}
	if _, present := entry["identity"].(map[string]any)["provider"]; present {
		t.Fatal("provider should be omitted when upstream kind is unset")
	}
}

func TestBuildContract_SeveralModelsIsAnArray(t *testing.T) {
	doc := roundTrip(t, buildContract("openai:chat-completions", []string{"gpt-x", "gpt-y"}, "completion_tokens",
		contractConfig{upstreamKind: "openai"}))
	entries, ok := doc.([]any)
	if !ok || len(entries) != 2 {
		t.Fatalf("want array of 2, got %T %v", doc, doc)
	}
	seen := map[string]bool{}
	for _, e := range entries {
		entry := e.(map[string]any)
		assertRelayable(t, entry)
		seen[entry["identity"].(map[string]any)["openai.model"].(string)] = true
		if entry["work_unit"].(map[string]any)["extractor"].(map[string]any)["field"] != "completion_tokens" {
			t.Fatalf("usage field not propagated: %v", entry["work_unit"])
		}
	}
	if !seen["gpt-x"] || !seen["gpt-y"] {
		t.Fatalf("identities = %v", seen)
	}
}

func TestNormalizeUsageField(t *testing.T) {
	for in, want := range map[string]string{"prompt_tokens": "prompt_tokens", "completion_tokens": "completion_tokens", "total_tokens": "total_tokens", "bogus": "total_tokens", "": "total_tokens"} {
		if got := normalizeUsageField(in); got != want {
			t.Fatalf("normalizeUsageField(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestHandleContract_AppliesAllowlistAndReadiness(t *testing.T) {
	cfg := config{capability: "openai:chat-completions", usageField: "total_tokens",
		modelAllowlist: modelAllowlist{"model-a": {}, "model-b": {}}, contract: contractConfig{upstreamKind: "vllm"}}

	var models atomic.Value
	rec := httptest.NewRecorder()
	handleContract(rec, httptest.NewRequest(http.MethodGet, contractPath, nil), cfg, &models)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("before discovery status = %d; want 503", rec.Code)
	}

	models.Store([]string{"first", "model-b", "disallowed", "model-a"})
	rec = httptest.NewRecorder()
	handleContract(rec, httptest.NewRequest(http.MethodGet, contractPath, nil), cfg, &models)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status/content-type = %d/%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	var entries []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("allowlisted pair should serialise as an array: %v\n%s", err, rec.Body.String())
	}
	var got []string
	for _, e := range entries {
		got = append(got, e["identity"].(map[string]any)["openai.model"].(string))
	}
	if !reflect.DeepEqual(got, []string{"model-b", "model-a"}) {
		t.Fatalf("identities = %v; want allowlisted models in upstream order", got)
	}

	rec = httptest.NewRecorder()
	handleContract(rec, httptest.NewRequest(http.MethodPost, contractPath, nil), cfg, &models)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d; want 405", rec.Code)
	}
}

func TestHandleModels_OpenAIListShape(t *testing.T) {
	cfg := config{modelAllowlist: modelAllowlist{"m1": {}}, contract: contractConfig{upstreamKind: "ollama"}}
	var models atomic.Value
	models.Store([]string{"m1", "m2"})
	rec := httptest.NewRecorder()
	handleModels(rec, httptest.NewRequest(http.MethodGet, modelsPath, nil), cfg, &models)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var list struct {
		Object string `json:"object"`
		Data   []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Object != "list" || len(list.Data) != 1 || list.Data[0].ID != "m1" || list.Data[0].Object != "model" || list.Data[0].OwnedBy != "ollama" {
		t.Fatalf("unexpected list: %s", rec.Body.String())
	}
}
