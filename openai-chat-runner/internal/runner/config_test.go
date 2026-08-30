package runner

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestConfigFromEnvVendorDefaults(t *testing.T) {
	for _, name := range []string{"UPSTREAM_KIND", "UPSTREAM_API_KEY", "MODEL_ALLOWLIST", "OUTPUT_TOKEN_WEIGHT"} {
		t.Setenv(name, "")
	}
	t.Setenv("UPSTREAM_URL", "http://upstream.test/v1/chat/completions")

	cfg, err := configFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.upstreamKind != upstreamVLLM || cfg.upstreamAPIKey != "" || cfg.modelAllowlist != nil || cfg.outputWeights != nil {
		t.Fatalf("unexpected unset defaults: kind=%q key=%q allowlist=%v weights=%v",
			cfg.upstreamKind, cfg.upstreamAPIKey, cfg.modelAllowlist, cfg.outputWeights)
	}
}

func TestConfigFromEnvRejectsInvalidUpstreamKindAndURL(t *testing.T) {
	t.Setenv("UPSTREAM_URL", "http://upstream.test/v1/chat/completions")
	t.Setenv("UPSTREAM_KIND", "ollama")
	if _, err := configFromEnv(); err == nil || !strings.Contains(err.Error(), "UPSTREAM_KIND") {
		t.Fatalf("invalid kind error = %v", err)
	}

	t.Setenv("UPSTREAM_KIND", "vllm")
	t.Setenv("UPSTREAM_URL", "://bad")
	if _, err := configFromEnv(); err == nil || !strings.Contains(err.Error(), "UPSTREAM_URL") {
		t.Fatalf("invalid URL error = %v", err)
	}
}

func TestParseModelAllowlist(t *testing.T) {
	got, err := parseModelAllowlist(" model-b,model-a ")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct{}{"model-a": {}, "model-b": {}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("allowlist = %#v; want %#v", got, want)
	}
	for _, raw := range []string{"model-a,", "model-a, model-a"} {
		if _, err := parseModelAllowlist(raw); err == nil {
			t.Errorf("parseModelAllowlist(%q) should fail", raw)
		}
	}
}

func TestParseOutputTokenWeights(t *testing.T) {
	got, err := parseOutputTokenWeights(" model-a=2,model-b=0 ")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]uint64{"model-a": 2, "model-b": 0}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("weights = %#v; want %#v", got, want)
	}
	for _, raw := range []string{"model-a", "model-a=-1", "model-a=1.5", "model-a=1,model-a=2", "=2"} {
		if _, err := parseOutputTokenWeights(raw); err == nil {
			t.Errorf("parseOutputTokenWeights(%q) should fail", raw)
		}
	}
}

func TestCalculateWorkUnits(t *testing.T) {
	usage := usageFields{PromptTokens: 12, CompletionTokens: 30, TotalTokens: 42}
	if got, ok := calculateWorkUnits(usage, "completion_tokens", "weighted", map[string]uint64{"weighted": 3}); !ok || got != 102 {
		t.Fatalf("weighted units = %d, %v; want 102, true", got, ok)
	}
	if got, ok := calculateWorkUnits(usage, "completion_tokens", "other", map[string]uint64{"weighted": 3}); !ok || got != 30 {
		t.Fatalf("fallback units = %d, %v; want 30, true", got, ok)
	}
	overflow := usageFields{PromptTokens: 1, CompletionTokens: math.MaxUint64}
	if _, ok := calculateWorkUnits(overflow, "total_tokens", "weighted", map[string]uint64{"weighted": 2}); ok {
		t.Fatal("overflow should be unrepresentable")
	}
}

func TestUpstreamBasePreservesVendorPrefix(t *testing.T) {
	cases := map[string]string{
		"https://api.openai.com/v1/chat/completions":                              "https://api.openai.com",
		"https://dashscope-intl.aliyuncs.com/compatible-mode/v1/chat/completions": "https://dashscope-intl.aliyuncs.com/compatible-mode",
		"http://vllm:8000/v1/chat/completions":                                    "http://vllm:8000",
	}
	for input, want := range cases {
		if got := upstreamBase(input); got != want {
			t.Errorf("upstreamBase(%q) = %q; want %q", input, got, want)
		}
	}
}
