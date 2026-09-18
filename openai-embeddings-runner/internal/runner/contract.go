package runner

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
)

// The runner contract (livepeer-network-protocol/protocols/runner-contract.md):
// the runner-owned half of a capability entry, served at
// GET /.well-known/livepeer-runner and relayed by the pool member agent.
// Replaces the old GET /<capability>/options surface, which nothing reads.
const (
	contractPath   = "/.well-known/livepeer-runner"
	modelsPath     = "/v1/models"
	protocolTag    = "paid-job/v1"
	paidJobVersion = "1.0.15"
	workUnitName   = "tokens"
)

// contractConfig is the operator-supplied metadata folded into the
// contract: the served identity and x-* facts about the backend.
type contractConfig struct {
	servedModelName     string
	backendModel        string
	embeddingDimensions int
	maxInputTokens      int
	poolingMode         string
	upstreamKind        string // "vllm" or "ollama"; becomes identity.provider
}

func contractConfigFromEnv() contractConfig {
	return contractConfig{
		servedModelName:     env("SERVED_MODEL_NAME", ""),
		backendModel:        env("BACKEND_MODEL", ""),
		embeddingDimensions: envInt("EMBEDDING_DIMENSIONS", 0),
		maxInputTokens:      envInt("MAX_INPUT_TOKENS", 0),
		poolingMode:         env("POOLING_MODE", ""),
		upstreamKind:        env("UPSTREAM_KIND", "vllm"),
	}
}

// contractModels: SERVED_MODEL_NAME is exactly one identity; otherwise
// every discovered model is its own entry, because the catalog matches
// on identity.openai.model.
func contractModels(models []string, cfg contractConfig) []string {
	if served := strings.TrimSpace(cfg.servedModelName); served != "" {
		return []string{served}
	}
	out := make([]string, 0, len(models))
	for _, m := range models {
		if m = strings.TrimSpace(m); m != "" {
			out = append(out, m)
		}
	}
	return out
}

func normalizeUsageField(field string) string {
	switch field {
	case "prompt_tokens", "completion_tokens":
		return field
	default:
		return "total_tokens"
	}
}

// buildContractEntry returns one capability entry for one served model,
// using only runner-attach §3.2 fields plus x-* extensions.
func buildContractEntry(capability, model, usageField string, cfg contractConfig) map[string]any {
	identity := map[string]string{"openai.model": model}
	if kind := strings.TrimSpace(cfg.upstreamKind); kind != "" {
		identity["provider"] = kind
	}
	entry := map[string]any{
		"capability_id": capability,
		"protocol":      protocolTag,
		"transports":    []string{"unary"},
		"work_unit": map[string]any{
			"name":      workUnitName,
			"extractor": map[string]any{"type": "openai-usage", "field": normalizeUsageField(usageField)},
		},
		"paths": map[string]string{"invoke": defaultEndpoint},
		"readiness": map[string]any{
			"type":   "http-openai-model-ready",
			"path":   modelsPath,
			"config": map[string]any{"model": model},
		},
		"identity":        identity,
		"schema_versions": map[string]string{protocolTag: paidJobVersion},
	}
	if cfg.backendModel != "" {
		entry["x-backend-model"] = cfg.backendModel
	}
	if cfg.embeddingDimensions > 0 {
		entry["x-embedding-dimensions"] = cfg.embeddingDimensions
	}
	if cfg.maxInputTokens > 0 {
		entry["x-max-input-tokens"] = cfg.maxInputTokens
	}
	if cfg.poolingMode != "" {
		entry["x-pooling-mode"] = cfg.poolingMode
	}
	return entry
}

// buildContract returns one entry object, or an array when the runner
// advertises more than one identity (runner-contract.md 1.1.0).
func buildContract(capability string, models []string, usageField string, cfg contractConfig) any {
	entries := make([]map[string]any, 0, len(models))
	for _, m := range contractModels(models, cfg) {
		entries = append(entries, buildContractEntry(capability, m, usageField, cfg))
	}
	if len(entries) == 1 {
		return entries[0]
	}
	return entries
}

func handleContract(w http.ResponseWriter, r *http.Request, capability, usageField string, cfg contractConfig, discoveredModels *atomic.Value) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	models, ok := loadModels(discoveredModels)
	if !ok {
		http.Error(w, "models not yet discovered", http.StatusServiceUnavailable)
		return
	}
	doc := buildContract(capability, models, usageField, cfg)
	if entries, isList := doc.([]map[string]any); isList && len(entries) == 0 {
		http.Error(w, "no model to advertise", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(doc)
}

// handleModels serves the OpenAI-shaped model list the readiness probe
// (http-openai-model-ready) reads.
func handleModels(w http.ResponseWriter, r *http.Request, cfg contractConfig, discoveredModels *atomic.Value) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	models, ok := loadModels(discoveredModels)
	if !ok {
		http.Error(w, "models not yet discovered", http.StatusServiceUnavailable)
		return
	}
	data := make([]map[string]any, 0, len(models))
	for _, m := range models {
		data = append(data, map[string]any{"id": m, "object": "model", "owned_by": strings.TrimSpace(cfg.upstreamKind)})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
}
