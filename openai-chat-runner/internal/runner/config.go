package runner

import (
	"fmt"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type upstreamKind string

type modelAllowlist map[string]struct{}

const (
	upstreamVLLM      upstreamKind = "vllm"
	upstreamOllama    upstreamKind = "ollama"
	upstreamOpenAI    upstreamKind = "openai"
	upstreamDashScope upstreamKind = "dashscope"
)

// config is loaded and validated once at startup. In particular, handlers do
// not consult the process environment, which keeps operator configuration
// stable for the lifetime of the process.
type config struct {
	addr             string
	upstreamURL      string
	upstreamKind     upstreamKind
	upstreamAPIKey   string
	capability       string
	usageField       string
	modelAllowlist   modelAllowlist
	outputWeights    map[string]uint64
	maxBodyBytes     int64
	discoveryRetries int
	contract         contractConfig
}

func configFromEnv() (config, error) {
	cfg := config{
		addr:             env("RUNNER_ADDR", ":8080"),
		upstreamURL:      strings.TrimSpace(os.Getenv("UPSTREAM_URL")),
		upstreamAPIKey:   strings.TrimSpace(os.Getenv("UPSTREAM_API_KEY")),
		capability:       env("CAPABILITY_NAME", defaultCapability),
		usageField:       env("USAGE_FIELD", "total_tokens"),
		maxBodyBytes:     defaultMaxBodyBytes,
		discoveryRetries: envInt("MODEL_DISCOVERY_RETRIES", 10),
		contract:         contractConfigFromEnv(),
	}
	if cfg.upstreamURL == "" {
		return config{}, fmt.Errorf("UPSTREAM_URL is required, e.g. http://HOST:PORT%s", defaultEndpoint)
	}
	parsedUpstream, parseErr := url.ParseRequestURI(cfg.upstreamURL)
	if parseErr != nil || (parsedUpstream.Scheme != "http" && parsedUpstream.Scheme != "https") || parsedUpstream.Host == "" {
		return config{}, fmt.Errorf("UPSTREAM_URL must be an absolute http or https URL")
	}

	kind := strings.TrimSpace(env("UPSTREAM_KIND", string(upstreamVLLM)))
	switch upstreamKind(kind) {
	case upstreamVLLM, upstreamOllama, upstreamOpenAI, upstreamDashScope:
		cfg.upstreamKind = upstreamKind(kind)
	default:
		return config{}, fmt.Errorf("UPSTREAM_KIND must be one of vllm, ollama, openai, or dashscope; got %q", kind)
	}
	cfg.contract.upstreamKind = string(cfg.upstreamKind)

	var err error
	cfg.modelAllowlist, err = parseModelAllowlist(os.Getenv("MODEL_ALLOWLIST"))
	if err != nil {
		return config{}, fmt.Errorf("MODEL_ALLOWLIST: %w", err)
	}
	cfg.outputWeights, err = parseOutputTokenWeights(os.Getenv("OUTPUT_TOKEN_WEIGHT"))
	if err != nil {
		return config{}, fmt.Errorf("OUTPUT_TOKEN_WEIGHT: %w", err)
	}
	if cfg.contract.servedModelName != "" && !cfg.modelAllowlist.allows(cfg.contract.servedModelName) {
		return config{}, fmt.Errorf("SERVED_MODEL_NAME %q is not present in MODEL_ALLOWLIST", cfg.contract.servedModelName)
	}
	return cfg, nil
}

func parseModelAllowlist(raw string) (modelAllowlist, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	models := make(modelAllowlist)
	for _, item := range strings.Split(raw, ",") {
		model := strings.TrimSpace(item)
		if model == "" {
			return nil, fmt.Errorf("contains an empty model ID")
		}
		if _, duplicate := models[model]; duplicate {
			return nil, fmt.Errorf("duplicate model ID %q", model)
		}
		models[model] = struct{}{}
	}
	return models, nil
}

// OUTPUT_TOKEN_WEIGHT uses model=weight entries. Work units are uint64 in the
// broker contract, so accepting fractional, negative, or overflowing weights
// would require silent rounding or overflow at request time. Reject them at
// startup instead.
func parseOutputTokenWeights(raw string) (map[string]uint64, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	weights := make(map[string]uint64)
	for _, item := range strings.Split(raw, ",") {
		parts := strings.Split(item, "=")
		if len(parts) != 2 {
			return nil, fmt.Errorf("entry %q must have model=weight form", strings.TrimSpace(item))
		}
		model, value := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		if model == "" || value == "" {
			return nil, fmt.Errorf("entry %q must contain a model and weight", strings.TrimSpace(item))
		}
		if _, duplicate := weights[model]; duplicate {
			return nil, fmt.Errorf("duplicate model ID %q", model)
		}
		weight, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("weight for model %q must be a non-negative integer representable as uint64", model)
		}
		weights[model] = weight
	}
	return weights, nil
}

func (a modelAllowlist) allows(model string) bool {
	if len(a) == 0 {
		return true
	}
	_, ok := a[model]
	return ok
}

// filter returns the discovered models that satisfy the policy, retaining
// upstream ordering. An unset policy returns the original slice so discovery
// and options preserve their existing behavior exactly.
func (a modelAllowlist) filter(discovered []string) []string {
	if len(a) == 0 {
		return discovered
	}
	filtered := make([]string, 0, len(discovered))
	for _, model := range discovered {
		if a.allows(model) {
			filtered = append(filtered, model)
		}
	}
	return filtered
}

func calculateWorkUnits(usage usageFields, usageField string, model string, weights map[string]uint64) (uint64, bool) {
	if weight, ok := weights[model]; ok {
		if weight != 0 && usage.CompletionTokens > (math.MaxUint64-usage.PromptTokens)/weight {
			return 0, false
		}
		return usage.PromptTokens + usage.CompletionTokens*weight, true
	}
	switch usageField {
	case "prompt_tokens":
		return usage.PromptTokens, true
	case "completion_tokens":
		return usage.CompletionTokens, true
	default:
		return usage.TotalTokens, true
	}
}
