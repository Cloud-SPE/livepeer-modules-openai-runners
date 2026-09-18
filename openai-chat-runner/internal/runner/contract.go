package runner

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
)

// The runner contract (livepeer-network-protocol/protocols/runner-contract.md).
//
// A runner says what it is: it serves the runner-owned half of a
// capability entry at GET /.well-known/livepeer-runner, the pool member
// agent reads it once per attach, adds local_id / devices / draining,
// and relays it to the broker. Nothing else describes this runner —
// the old GET /<capability>/options surface is gone.
const (
	contractPath   = "/.well-known/livepeer-runner"
	modelsPath     = "/v1/models"
	protocolTag    = "paid-job/v1"
	paidJobVersion = "1.0.15"
	workUnitName   = "tokens"
)

// contractConfig is the operator-supplied metadata the runner folds into
// its contract: the served identity and the x-* facts about the backend.
// Unset fields are omitted.
type contractConfig struct {
	servedModelName string
	backendModel    string
	contextLength   int
	reasoningParser string
	toolCallParser  string
	quantization    string
	upstreamKind    string // bounded vendor kind; becomes identity.provider
}

func contractConfigFromEnv() contractConfig {
	return contractConfig{
		servedModelName: env("SERVED_MODEL_NAME", ""),
		backendModel:    env("BACKEND_MODEL", ""),
		contextLength:   envInt("CONTEXT_LENGTH", 0),
		reasoningParser: env("REASONING_PARSER", ""),
		toolCallParser:  env("TOOL_CALL_PARSER", ""),
		quantization:    env("QUANTIZATION", ""),
	}
}

// contractModels decides which identities the runner advertises. An
// operator-declared SERVED_MODEL_NAME is exactly one identity, whatever
// the upstream lists. Otherwise every discovered (allowlist-filtered)
// model is its own entry — the catalog matches on identity.openai.model,
// so a vendor-backed runner exposing three models needs three entries.
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

// buildContractEntry returns one capability entry for one served model.
// Keys are exactly the runner-owned fields of runner-attach §3.2 plus
// x-* extensions; anything else would reject the whole contract.
func buildContractEntry(capability, model, usageField string, cfg contractConfig) map[string]any {
	identity := map[string]string{"openai.model": model}
	if kind := strings.TrimSpace(cfg.upstreamKind); kind != "" {
		identity["provider"] = kind
	}
	entry := map[string]any{
		"capability_id": capability,
		"protocol":      protocolTag,
		"transports":    []string{"unary", "stream"},
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
	if cfg.contextLength > 0 {
		entry["x-context-length"] = cfg.contextLength
	}
	if cfg.quantization != "" {
		entry["x-quantization"] = cfg.quantization
	}
	if cfg.reasoningParser != "" {
		entry["x-reasoning-parser"] = cfg.reasoningParser
	}
	if cfg.toolCallParser != "" {
		entry["x-tool-call-parser"] = cfg.toolCallParser
	}
	return entry
}

// normalizeUsageField maps USAGE_FIELD onto the openai-usage extractor's
// accepted values; anything else falls back to total_tokens, which is
// also what the runner's own counting does.
func normalizeUsageField(field string) string {
	switch field {
	case "prompt_tokens", "completion_tokens":
		return field
	default:
		return "total_tokens"
	}
}

// buildContract returns the document served at /.well-known/livepeer-runner:
// a single entry object, or an array when the runner advertises more than
// one identity (runner-contract.md §3, 1.1.0).
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

func handleContract(w http.ResponseWriter, r *http.Request, cfg config, discoveredModels *atomic.Value) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	models, ok := loadModels(discoveredModels)
	if !ok {
		http.Error(w, "models not yet discovered", http.StatusServiceUnavailable)
		return
	}
	models = cfg.modelAllowlist.filter(models)
	doc := buildContract(cfg.capability, models, cfg.usageField, cfg.contract)
	if entries, isList := doc.([]map[string]any); isList && len(entries) == 0 {
		http.Error(w, "no model to advertise", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(doc)
}

// handleModels serves the OpenAI-shaped model list for the runner's own
// readiness probe (http-openai-model-ready reads it) and for callers
// that want to know what is behind the proxy. It reflects discovery
// after MODEL_ALLOWLIST, never the raw upstream list.
func handleModels(w http.ResponseWriter, r *http.Request, cfg config, discoveredModels *atomic.Value) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	models, ok := loadModels(discoveredModels)
	if !ok {
		http.Error(w, "models not yet discovered", http.StatusServiceUnavailable)
		return
	}
	models = cfg.modelAllowlist.filter(models)
	data := make([]map[string]any, 0, len(models))
	for _, m := range models {
		data = append(data, map[string]any{"id": m, "object": "model", "owned_by": strings.TrimSpace(cfg.contract.upstreamKind)})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
}
