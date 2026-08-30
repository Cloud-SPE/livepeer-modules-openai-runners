package runner

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
)

func TestBuildOptionsPayload_Minimal(t *testing.T) {
	out := buildOptionsPayload([]string{"M"}, optionsConfig{})
	if out["task"] != "chat" {
		t.Fatalf("task = %v", out["task"])
	}
	if !reflect.DeepEqual(out["models"], []string{"M"}) {
		t.Fatalf("models = %v", out["models"])
	}
	if out["served_model_name"] != "M" {
		t.Fatalf("served_model_name should default to first model; got %v", out["served_model_name"])
	}
	if _, ok := out["backend_model"]; ok {
		t.Fatal("backend_model should be omitted when unset")
	}
	if _, ok := out["context_length"]; ok {
		t.Fatal("context_length should be omitted when zero")
	}
	if _, ok := out["parsers"]; ok {
		t.Fatal("parsers should be omitted when no parsers declared")
	}
	features, ok := out["features"].(map[string]any)
	if !ok {
		t.Fatalf("features missing or wrong type: %T", out["features"])
	}
	if features["streaming"] != true {
		t.Fatal("streaming feature should always be true")
	}
	if features["include_usage_required"] != true {
		t.Fatal("include_usage_required feature should always be true")
	}
	if _, ok := features["tool_calling"]; ok {
		t.Fatal("tool_calling should not be set when TOOL_CALL_PARSER is empty")
	}
	if _, ok := features["reasoning"]; ok {
		t.Fatal("reasoning should not be set when REASONING_PARSER is empty")
	}
}

func TestBuildOptionsPayload_Full(t *testing.T) {
	cfg := optionsConfig{
		servedModelName: "Qwen3.6-27B",
		backendModel:    "sakamakismile/Qwen3.6-27B-Text-NVFP4-MTP",
		contextLength:   196608,
		reasoningParser: "qwen3",
		toolCallParser:  "qwen3_coder",
		quantization:    "modelopt",
	}
	out := buildOptionsPayload([]string{"Qwen3.6-27B"}, cfg)
	if out["served_model_name"] != "Qwen3.6-27B" {
		t.Fatalf("served_model_name = %v", out["served_model_name"])
	}
	if out["backend_model"] != "sakamakismile/Qwen3.6-27B-Text-NVFP4-MTP" {
		t.Fatalf("backend_model = %v", out["backend_model"])
	}
	if out["context_length"] != 196608 {
		t.Fatalf("context_length = %v", out["context_length"])
	}
	if out["quantization"] != "modelopt" {
		t.Fatalf("quantization = %v", out["quantization"])
	}
	parsers, ok := out["parsers"].(map[string]any)
	if !ok {
		t.Fatalf("parsers missing or wrong type: %T", out["parsers"])
	}
	if parsers["reasoning"] != "qwen3" {
		t.Fatalf("reasoning parser = %v", parsers["reasoning"])
	}
	if parsers["tool_call"] != "qwen3_coder" {
		t.Fatalf("tool_call parser = %v", parsers["tool_call"])
	}
	features := out["features"].(map[string]any)
	if features["tool_calling"] != true {
		t.Fatal("tool_calling should be true when TOOL_CALL_PARSER is set")
	}
	if features["reasoning"] != true {
		t.Fatal("reasoning should be true when REASONING_PARSER is set")
	}
}

func TestBuildOptionsPayload_OperatorOverridesDiscoveredModel(t *testing.T) {
	cfg := optionsConfig{servedModelName: "operator-chosen"}
	out := buildOptionsPayload([]string{"discovered-from-vllm"}, cfg)
	if out["served_model_name"] != "operator-chosen" {
		t.Fatalf("operator-set SERVED_MODEL_NAME should win over discovery; got %v", out["served_model_name"])
	}
}

func TestBuildOptionsPayload_AdvertisesUpstreamKind(t *testing.T) {
	for _, kind := range []string{"vllm", "openai", "dashscope"} {
		out := buildOptionsPayload([]string{"m"}, optionsConfig{upstreamKind: kind})
		if got := out["upstream_kind"]; got != kind {
			t.Errorf("kind=%s: got %v, want %s", kind, got, kind)
		}
	}
}

func TestBuildOptionsPayload_OmitsUpstreamKindWhenEmpty(t *testing.T) {
	out := buildOptionsPayload([]string{"m"}, optionsConfig{})
	if _, present := out["upstream_kind"]; present {
		t.Fatal("upstream_kind should be omitted when unset")
	}
}

func TestHandleOptionsAppliesModelAllowlist(t *testing.T) {
	tests := []struct {
		name      string
		allowlist modelAllowlist
		want      []string
	}{
		{
			name:      "configured filters without inventing missing models",
			allowlist: modelAllowlist{"model-a": {}, "model-b": {}, "not-discovered": {}},
			want:      []string{"model-b", "model-a"},
		},
		{
			name: "unset preserves discovered models",
			want: []string{"first", "model-b", "disallowed", "model-a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			models := atomic.Value{}
			models.Store([]string{"first", "model-b", "disallowed", "model-a"})
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/openai-chat-completions/options", nil)
			handleOptions(rec, req, config{modelAllowlist: tt.allowlist}, &models)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d; want 200", rec.Code)
			}
			var payload struct {
				Models          []string `json:"models"`
				ServedModelName string   `json:"served_model_name"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(payload.Models, tt.want) {
				t.Fatalf("models = %v; want %v", payload.Models, tt.want)
			}
			if payload.ServedModelName != tt.want[0] {
				t.Fatalf("served_model_name = %q; want %q", payload.ServedModelName, tt.want[0])
			}
		})
	}
}
