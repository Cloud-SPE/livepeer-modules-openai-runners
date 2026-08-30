// Package runner implements the openai-chat-runner: a proxy that sits
// between the capability broker and a vLLM (or OpenAI-compatible)
// chat-completions backend. Its added value over the transparent
// openai-runner is streaming-aware token counting:
//
//   - Streaming requests: the runner scans the upstream SSE response
//     for a final `usage` block (emitted by vLLM when
//     `stream_options.include_usage: true`), then declares
//     `X-Livepeer-Work-Units` as an HTTP trailer and emits the token
//     count after the body. The broker's response-trailer extractor
//     reads this trailer.
//
//   - Non-streaming requests: the runner passes the response through
//     unchanged. The broker uses its usual openai-usage extractor to
//     read `usage.total_tokens` from the JSON body.
//
// Auto-injects `stream_options.include_usage: true` on streaming
// requests when absent, so clients don't have to know about the
// billing requirement.
package runner

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

const (
	defaultEndpoint     = "/v1/chat/completions"
	defaultCapability   = "openai-chat-completions"
	defaultMaxBodyBytes = int64(5 << 20)
	workUnitsTrailer    = "X-Livepeer-Work-Units"
	runnerErrorHeader   = "X-Livepeer-Runner-Error"
)

// Run starts the runner with environment-driven config and blocks.
func Run() {
	cfg, err := configFromEnv()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	client := &http.Client{Transport: newTransport()}

	var discoveredModels atomic.Value
	go func() {
		ids, err := discoverModelsWithRetryConfig(client, upstreamBase(cfg.upstreamURL), cfg, cfg.discoveryRetries, 10*time.Second)
		if err != nil {
			log.Fatalf("model discovery failed: %v", err)
		}
		discoveredModels.Store(ids)
		slog.Info("models discovered", "upstream_kind", cfg.upstreamKind, "count", len(ids))
	}()

	mux := http.NewServeMux()

	mux.HandleFunc(defaultEndpoint, func(w http.ResponseWriter, r *http.Request) {
		handleChatCompletionsWithConfig(w, r, client, cfg, &discoveredModels)
	})

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		models, ok := loadModels(&discoveredModels)
		if !ok {
			http.Error(w, "models not yet discovered", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "models": models})
	})

	mux.HandleFunc("/"+cfg.capability+"/options", func(w http.ResponseWriter, r *http.Request) {
		handleOptions(w, r, cfg, &discoveredModels)
	})

	slog.Info("openai-chat-runner listening", "addr", cfg.addr, "capability", cfg.capability,
		"upstream", cfg.upstreamURL, "upstream_kind", cfg.upstreamKind, "usage_field", cfg.usageField)
	srv := &http.Server{
		Addr:              cfg.addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Fatal(srv.ListenAndServe())
}

// handleChatCompletions retains the original test-facing helper with the
// legacy defaults. Production passes the startup-loaded typed configuration to
// handleChatCompletionsWithConfig.
func handleChatCompletions(w http.ResponseWriter, r *http.Request, client *http.Client, upstream string, maxBodyBytes int64, usageField string, discoveredModels *atomic.Value) {
	handleChatCompletionsWithConfig(w, r, client, config{
		upstreamURL: upstream, upstreamKind: upstreamVLLM, maxBodyBytes: maxBodyBytes, usageField: usageField,
	}, discoveredModels)
}

func handleChatCompletionsWithConfig(w http.ResponseWriter, r *http.Request, client *http.Client, cfg config, discoveredModels *atomic.Value) {
	started := time.Now()
	observed := &statusResponseWriter{ResponseWriter: w}
	w = observed
	defer func() {
		slog.Info("request completed", "method", r.Method, "path", r.URL.Path,
			"upstream_kind", cfg.upstreamKind, "status", observed.statusCode(),
			"duration_seconds", time.Since(started).Seconds())
	}()

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, ok := loadModels(discoveredModels); !ok {
		http.Error(w, "model not yet ready", http.StatusServiceUnavailable)
		return
	}

	ctx := r.Context()
	if lp, ok := decodeLivepeerHeader(r.Header.Get("Livepeer")); ok && lp.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(lp.TimeoutSeconds)*time.Second)
		defer cancel()
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, cfg.maxBodyBytes))
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	_ = r.Body.Close()

	// Auto-inject include_usage on streaming requests. Non-streaming
	// requests pass through untouched.
	model, err := requestModel(bodyBytes)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "request body is not valid JSON", "invalid_request_error")
		return
	}
	if !cfg.modelAllowlist.allows(model) {
		writeOpenAIError(w, http.StatusBadRequest, fmt.Sprintf("model %q is not allowed", model), "invalid_request_error")
		return
	}

	rewritten, isStream, err := ensureIncludeUsage(bodyBytes)
	if err != nil {
		http.Error(w, "request body is not valid JSON", http.StatusBadRequest)
		return
	}
	bodyBytes = rewritten

	req, err := newUpstreamRequest(ctx, http.MethodPost, cfg.upstreamURL, bytes.NewReader(bodyBytes), cfg)
	if err != nil {
		http.Error(w, "failed to create upstream request", http.StatusBadGateway)
		return
	}
	req.ContentLength = int64(len(bodyBytes))
	copyHeader(req.Header, r.Header, []string{"Content-Type", "Accept"})
	req.Header.Del("Livepeer")

	resp, err := client.Do(req)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "context deadline exceeded") {
			status = http.StatusGatewayTimeout
		}
		slog.Error("upstream request failed", "upstream_kind", cfg.upstreamKind, "status", status)
		writeRunnerError(w, status, "upstream request failed")
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if isRefundableUpstreamStatus(resp.StatusCode) {
		slog.Error("upstream vendor error", "upstream_kind", cfg.upstreamKind, "status", resp.StatusCode,
			"vendor_request_id", vendorRequestID(resp.Header))
		writeRunnerError(w, resp.StatusCode, "upstream request failed")
		return
	}

	if isStream && guardSSEContentType(resp.Header) == nil {
		writeStreamingResponseWeighted(w, resp, cfg.usageField, model, cfg.outputWeights)
		return
	}
	writePassThroughResponseWeighted(w, resp, cfg.usageField, model, cfg.outputWeights)
}

// statusResponseWriter records the broker-facing status for the structured
// request log while retaining streaming flush support.
type statusResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusResponseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusResponseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(p)
}

func (w *statusResponseWriter) Flush() {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *statusResponseWriter) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

// writeStreamingResponse forwards an SSE response while counting
// tokens, then emits X-Livepeer-Work-Units as an HTTP trailer.
func writeStreamingResponse(w http.ResponseWriter, resp *http.Response, usageField string) {
	writeStreamingResponseWeighted(w, resp, usageField, "", nil)
}

func writeStreamingResponseWeighted(w http.ResponseWriter, resp *http.Response, usageField, model string, weights map[string]uint64) {
	// Declare the trailer BEFORE WriteHeader; Go's server promotes
	// declared trailers to the wire trailer slot when set after the
	// body. Tracks the broker's http-stream driver pattern.
	w.Header().Set("Trailer", workUnitsTrailer)
	copyAllHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)

	flusher, _ := w.(http.Flusher)
	flush := func() {
		if flusher != nil {
			flusher.Flush()
		}
	}

	total := streamAndCalculateUsage(w, resp.Body, usageField, model, weights, flush)
	w.Header().Set(workUnitsTrailer, fmt.Sprintf("%d", total))
}

// writePassThroughResponse copies a non-streaming response (or an unexpected
// non-SSE response) to the client. For an unweighted model, token counting is
// left to the broker's openai-usage extractor reading the body.
func writePassThroughResponse(w http.ResponseWriter, resp *http.Response) {
	copyAllHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			_, _ = w.Write(buf[:n])
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}

func writePassThroughResponseWeighted(w http.ResponseWriter, resp *http.Response, usageField, model string, weights map[string]uint64) {
	if _, weighted := weights[model]; !weighted {
		writePassThroughResponse(w, resp)
		return
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		writeRunnerError(w, http.StatusBadGateway, "upstream response failed")
		return
	}
	units, err := extractWorkUnits(body, usageField, model, weights)
	if err != nil {
		writeRunnerError(w, http.StatusBadGateway, "upstream response contained invalid usage")
		return
	}
	copyAllHeaders(w.Header(), resp.Header)
	w.Header().Set(workUnitsTrailer, fmt.Sprintf("%d", units))
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}

func requestModel(body []byte) (string, error) {
	var request struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		return "", err
	}
	return request.Model, nil
}

func extractWorkUnits(body []byte, usageField, model string, weights map[string]uint64) (uint64, error) {
	var envelope usageEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return 0, err
	}
	if envelope.Usage == nil {
		return 0, fmt.Errorf("missing usage")
	}
	usage := *envelope.Usage
	units, representable := calculateWorkUnits(usage, usageField, model, weights)
	if !representable {
		return 0, fmt.Errorf("weighted usage overflows uint64")
	}
	return units, nil
}

func isRefundableUpstreamStatus(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusTooManyRequests || status >= 500
}

func writeRunnerError(w http.ResponseWriter, status int, message string) {
	w.Header().Set(runnerErrorHeader, "true")
	writeOpenAIError(w, status, message, "upstream_error")
}

func writeOpenAIError(w http.ResponseWriter, status int, message, errorType string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": message, "type": errorType}})
}

func vendorRequestID(header http.Header) string {
	for _, name := range []string{"X-Request-ID", "Request-ID", "X-Dashscope-Request-ID"} {
		if value := header.Get(name); value != "" {
			return value
		}
	}
	return ""
}

// optionsConfig captures the operator-supplied metadata the runner
// surfaces via /<capability>/options. These map 1:1 to the fields the
// broker's chat-options discovery merges into the capability's `extra`
// block. Unset fields are simply omitted from the response so the
// broker can fall back to any host-config-declared values.
type optionsConfig struct {
	servedModelName string
	backendModel    string
	contextLength   int
	reasoningParser string
	toolCallParser  string
	quantization    string
	upstreamKind    string // bounded vendor kind; advertised in payload
}

func optionsConfigFromEnv() optionsConfig {
	return optionsConfig{
		servedModelName: env("SERVED_MODEL_NAME", ""),
		backendModel:    env("BACKEND_MODEL", ""),
		contextLength:   envInt("CONTEXT_LENGTH", 0),
		reasoningParser: env("REASONING_PARSER", ""),
		toolCallParser:  env("TOOL_CALL_PARSER", ""),
		quantization:    env("QUANTIZATION", ""),
	}
}

// buildOptionsPayload returns the structured /<capability>/options
// payload the broker's chat-options discovery reads. Mirrors the shape
// of the audio/video options endpoints so the broker can hydrate the
// capability's `extra` block declaratively.
//
// The runner advertises:
//   - models / served_model_name — sourced from vLLM `/v1/models` and
//     the operator-supplied SERVED_MODEL_NAME (operator wins).
//   - backend_model — HuggingFace path or other upstream identifier.
//   - context_length — operator-declared max model length.
//   - parsers — operator-declared reasoning / tool-call parsers.
//   - quantization — operator-declared quantization scheme.
//   - features — derived booleans: streaming is always true (this
//     runner exists to count streaming tokens), include_usage_required
//     is always true (vLLM only emits usage when the flag is set;
//     this runner injects it for clients), tool_calling/reasoning are
//     derived from whether the operator declared a parser.
func buildOptionsPayload(models []string, cfg optionsConfig) map[string]any {
	out := map[string]any{
		"task":   "chat",
		"models": models,
	}
	if kind := strings.TrimSpace(cfg.upstreamKind); kind != "" {
		out["upstream_kind"] = kind
	}

	served := cfg.servedModelName
	if served == "" && len(models) > 0 {
		served = models[0]
	}
	if served != "" {
		out["served_model_name"] = served
	}
	if cfg.backendModel != "" {
		out["backend_model"] = cfg.backendModel
	}
	if cfg.contextLength > 0 {
		out["context_length"] = cfg.contextLength
	}
	if cfg.quantization != "" {
		out["quantization"] = cfg.quantization
	}

	parsers := map[string]any{}
	if cfg.reasoningParser != "" {
		parsers["reasoning"] = cfg.reasoningParser
	}
	if cfg.toolCallParser != "" {
		parsers["tool_call"] = cfg.toolCallParser
	}
	if len(parsers) > 0 {
		out["parsers"] = parsers
	}

	features := map[string]any{
		"streaming":              true,
		"include_usage_required": true,
	}
	if cfg.reasoningParser != "" {
		features["reasoning"] = true
	}
	if cfg.toolCallParser != "" {
		features["tool_calling"] = true
	}
	out["features"] = features

	return out
}

func handleOptions(w http.ResponseWriter, r *http.Request, cfg config, discoveredModels *atomic.Value) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	models, _ := loadModels(discoveredModels)
	models = cfg.modelAllowlist.filter(models)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(buildOptionsPayload(models, cfg.options))
}

type livepeerHeader struct {
	Request        string `json:"request"`
	Capability     string `json:"capability"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

func newTransport() *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          200,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

func loadModels(v *atomic.Value) ([]string, bool) {
	if loaded := v.Load(); loaded != nil {
		return loaded.([]string), true
	}
	return nil, false
}

func upstreamBase(upstream string) string {
	u, err := url.Parse(upstream)
	if err != nil {
		return upstream
	}
	const completionSuffix = "/v1/chat/completions"
	if strings.HasSuffix(strings.TrimRight(u.Path, "/"), completionSuffix) {
		u.Path = strings.TrimSuffix(strings.TrimRight(u.Path, "/"), completionSuffix)
	} else {
		u.Path = strings.TrimRight(u.Path, "/")
	}
	u.RawPath = ""
	u.RawQuery = ""
	return u.String()
}

func discoverModelsWithRetry(base string, retries int, delay time.Duration) ([]string, error) {
	return discoverModelsWithRetryConfig(http.DefaultClient, base, config{}, retries, delay)
}

func discoverModelsWithRetryConfig(client *http.Client, base string, cfg config, retries int, delay time.Duration) ([]string, error) {
	for i := 0; i < retries; i++ {
		if i > 0 {
			time.Sleep(delay)
		}
		ids, err := discoverModelsWithConfig(client, base, cfg)
		if err == nil {
			return ids, nil
		}
		attrs := []any{"upstream_kind", cfg.upstreamKind, "attempt", i + 1, "attempts", retries}
		var upstreamErr *upstreamRequestError
		if errors.As(err, &upstreamErr) {
			attrs = append(attrs, "status", upstreamErr.status, "vendor_request_id", upstreamErr.vendorRequestID)
		}
		slog.Warn("model discovery failed", attrs...)
	}
	return nil, fmt.Errorf("model discovery failed after %d attempts", retries)
}

func discoverModels(base string) ([]string, error) {
	return discoverModelsWithConfig(http.DefaultClient, base, config{})
}

func discoverModelsWithConfig(client *http.Client, base string, cfg config) ([]string, error) {
	req, err := newUpstreamRequest(context.Background(), http.MethodGet, base+"/v1/models", nil, cfg)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, &upstreamRequestError{status: resp.StatusCode, vendorRequestID: vendorRequestID(resp.Header)}
	}
	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode /v1/models response: %w", err)
	}
	if len(result.Data) == 0 {
		return nil, fmt.Errorf("no models returned from %s/v1/models", base)
	}
	ids := make([]string, 0, len(result.Data))
	for _, m := range result.Data {
		ids = append(ids, m.ID)
	}
	ids = cfg.modelAllowlist.filter(ids)
	if len(ids) == 0 {
		return nil, fmt.Errorf("no upstream models matched MODEL_ALLOWLIST")
	}
	return ids, nil
}

// upstreamRequestError carries only the bounded metadata safe to expose in
// logs. It intentionally cannot retain an upstream body or authorization.
type upstreamRequestError struct {
	status          int
	vendorRequestID string
}

func (e *upstreamRequestError) Error() string {
	return fmt.Sprintf("unexpected upstream status %d", e.status)
}

// newUpstreamRequest is the single construction path for discovery and chat
// requests. Only the startup-loaded operator credential can become outbound
// Authorization; inbound credentials are never passed to this function.
func newUpstreamRequest(ctx context.Context, method, target string, body io.Reader, cfg config) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, err
	}
	if cfg.upstreamAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.upstreamAPIKey)
	}
	return req, nil
}

func env(k, def string) string {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	return v
}

func envInt(k string, def int) int {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n <= 0 {
		return def
	}
	return n
}

func decodeLivepeerHeader(v string) (livepeerHeader, bool) {
	var lp livepeerHeader
	if v == "" {
		return lp, false
	}
	raw, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		return lp, false
	}
	if err := json.Unmarshal(raw, &lp); err != nil {
		return lp, false
	}
	return lp, true
}

func copyHeader(dst http.Header, src http.Header, keys []string) {
	for _, k := range keys {
		if v := src.Get(k); v != "" {
			dst.Set(k, v)
		}
	}
}

func copyAllHeaders(dst http.Header, src http.Header) {
	for k, vv := range src {
		if strings.EqualFold(k, "Connection") ||
			strings.EqualFold(k, "Keep-Alive") ||
			strings.EqualFold(k, "Proxy-Authenticate") ||
			strings.EqualFold(k, "Proxy-Authorization") ||
			strings.EqualFold(k, "TE") ||
			strings.EqualFold(k, "Trailer") ||
			strings.EqualFold(k, "Transfer-Encoding") ||
			strings.EqualFold(k, "Upgrade") {
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}
