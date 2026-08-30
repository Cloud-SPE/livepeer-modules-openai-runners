package runner

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeUpstream returns an httptest.Server that responds with a fixed
// SSE payload. The Content-Type header marks it as an event stream so
// the runner takes the streaming path.
func fakeUpstream(t *testing.T, payload string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, payload)
	}))
}

func newConfiguredRunner(t *testing.T, upstreamURL string, configure func(*config)) http.Handler {
	t.Helper()
	models := atomic.Value{}
	models.Store([]string{"allowed", "other"})
	cfg := config{
		upstreamURL:  upstreamURL,
		upstreamKind: upstreamOpenAI,
		maxBodyBytes: defaultMaxBodyBytes,
		usageField:   "total_tokens",
	}
	configure(&cfg)
	mux := http.NewServeMux()
	mux.HandleFunc(defaultEndpoint, func(w http.ResponseWriter, r *http.Request) {
		handleChatCompletionsWithConfig(w, r, http.DefaultClient, cfg, &models)
	})
	return mux
}

func newReadyRunner(t *testing.T, upstreamURL string) http.Handler {
	t.Helper()
	models := atomic.Value{}
	models.Store([]string{"test-model"})
	client := &http.Client{Transport: newTransport()}
	mux := http.NewServeMux()
	mux.HandleFunc(defaultEndpoint, func(w http.ResponseWriter, r *http.Request) {
		handleChatCompletions(w, r, client, upstreamURL, defaultMaxBodyBytes, "total_tokens", &models)
	})
	return mux
}

func TestHandler_StreamingEmitsTrailer(t *testing.T) {
	upstream := fakeUpstream(t, vllmStreamFixture)
	t.Cleanup(upstream.Close)

	handler := newReadyRunner(t, upstream.URL)

	body := strings.NewReader(`{"model":"test-model","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	req := httptest.NewRequest(http.MethodPost, defaultEndpoint, body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", rec.Code)
	}
	// Response body should include the SSE frames the upstream sent.
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"content":"Hello"`)) {
		t.Fatalf("response body missing forwarded content: %s", rec.Body.String())
	}
	// Trailer should carry the work-units value.
	// httptest.ResponseRecorder exposes trailers on its Header() map
	// because they're declared via the Trailer response header.
	gotTrailer := rec.Header().Get(workUnitsTrailer)
	if gotTrailer != "42" {
		t.Fatalf("trailer %s = %q; want %q", workUnitsTrailer, gotTrailer, "42")
	}
}

func TestHandler_NonStreamingPassesThrough(t *testing.T) {
	const responseBody = "  {\n  \"id\": \"x\", \"usage\": {\"total_tokens\": 17}\n}\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, responseBody)
	}))
	t.Cleanup(upstream.Close)

	handler := newReadyRunner(t, upstream.URL)
	body := strings.NewReader(`{"model":"test-model","messages":[{"role":"user","content":"hi"}]}`)
	req := httptest.NewRequest(http.MethodPost, defaultEndpoint, body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", rec.Code)
	}
	if rec.Body.String() != responseBody {
		t.Fatalf("response body changed with OUTPUT_TOKEN_WEIGHT unset:\n got: %q\nwant: %q", rec.Body.String(), responseBody)
	}
	// No trailer should be set on non-streaming responses; the broker
	// uses openai-usage on the body for those.
	if got := rec.Header().Get(workUnitsTrailer); got != "" {
		t.Fatalf("non-streaming response should not set work-units trailer; got %q", got)
	}
}

func TestHandler_SetsOnlyOperatorAuthorization(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer operator-key" {
			t.Errorf("Authorization = %q; want exact operator bearer", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"usage":{"total_tokens":1}}`)
	}))
	t.Cleanup(upstream.Close)
	handler := newConfiguredRunner(t, upstream.URL, func(cfg *config) { cfg.upstreamAPIKey = "operator-key" })
	req := httptest.NewRequest(http.MethodPost, defaultEndpoint, strings.NewReader(`{"model":"allowed"}`))
	req.Header.Set("Authorization", "Bearer customer-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", rec.Code)
	}
}

func TestOutboundRequestsHaveNoAuthorizationByDefault(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q; want absent", got)
		}
		switch r.URL.Path {
		case "/v1/models":
			_, _ = io.WriteString(w, `{"data":[{"id":"allowed"}]}`)
		case defaultEndpoint:
			_, _ = io.WriteString(w, `{"usage":{"total_tokens":1}}`)
		default:
			t.Errorf("unexpected upstream path %q", r.URL.Path)
		}
	}))
	t.Cleanup(upstream.Close)

	if _, err := discoverModelsWithConfig(http.DefaultClient, upstream.URL, config{}); err != nil {
		t.Fatal(err)
	}
	handler := newConfiguredRunner(t, upstream.URL+defaultEndpoint, func(cfg *config) {
		cfg.upstreamKind = upstreamVLLM
	})
	req := httptest.NewRequest(http.MethodPost, defaultEndpoint, strings.NewReader(`{"model":"allowed"}`))
	req.Header.Set("Authorization", "Bearer inbound-customer-secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || calls.Load() != 2 {
		t.Fatalf("status=%d calls=%d; want 200 and 2", rec.Code, calls.Load())
	}
}

func TestHandler_RejectsDisallowedModelBeforeProxying(t *testing.T) {
	called := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true }))
	t.Cleanup(upstream.Close)
	handler := newConfiguredRunner(t, upstream.URL, func(cfg *config) {
		cfg.modelAllowlist = map[string]struct{}{"allowed": {}}
	})
	req := httptest.NewRequest(http.MethodPost, defaultEndpoint, strings.NewReader(`{"model":"other"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || called {
		t.Fatalf("status = %d, upstream called = %v; want 400, false", rec.Code, called)
	}
	if got := rec.Header().Get(runnerErrorHeader); got != "" {
		t.Fatalf("runner error header = %q; policy rejection is not an upstream failure", got)
	}
	if got := rec.Body.String(); !strings.Contains(got, `model \"other\" is not allowed`) ||
		!strings.Contains(got, `"type":"invalid_request_error"`) {
		t.Fatalf("unexpected policy error body: %s", got)
	}
}

func TestHandler_VendorFailureIsSanitizedAndRefundable(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"secret_vendor_body":"do not return"}`)
	}))
	t.Cleanup(upstream.Close)
	handler := newConfiguredRunner(t, upstream.URL, func(_ *config) {})
	req := httptest.NewRequest(http.MethodPost, defaultEndpoint, strings.NewReader(`{"model":"allowed"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d; want 429", rec.Code)
	}
	if got := rec.Header().Get(runnerErrorHeader); got != "true" {
		t.Fatalf("runner error header = %q; want true", got)
	}
	if strings.Contains(rec.Body.String(), "secret_vendor_body") {
		t.Fatalf("vendor body leaked: %s", rec.Body.String())
	}
}

func TestHandler_RefundableFailuresLogOnlySanitizedMetadata(t *testing.T) {
	const (
		apiKey      = "operator-secret-key"
		vendorBody  = "highly-sensitive-vendor-body"
		vendorReqID = "vendor-request-123"
	)
	for _, status := range []int{
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
			t.Cleanup(func() { slog.SetDefault(previous) })

			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("Authorization"); got != "Bearer "+apiKey {
					t.Errorf("Authorization = %q", got)
				}
				w.Header().Set("X-Request-ID", vendorReqID)
				w.WriteHeader(status)
				_, _ = io.WriteString(w, vendorBody)
			}))
			t.Cleanup(upstream.Close)
			handler := newConfiguredRunner(t, upstream.URL, func(cfg *config) {
				cfg.upstreamKind = upstreamDashScope
				cfg.upstreamAPIKey = apiKey
			})
			req := httptest.NewRequest(http.MethodPost, defaultEndpoint, strings.NewReader(`{"model":"allowed"}`))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != status || rec.Header().Get(runnerErrorHeader) != "true" {
				t.Fatalf("status=%d runner-error=%q; want %d,true", rec.Code, rec.Header().Get(runnerErrorHeader), status)
			}
			combined := logs.String() + rec.Body.String()
			if strings.Contains(combined, apiKey) || strings.Contains(combined, vendorBody) {
				t.Fatalf("secret or vendor body leaked: %s", combined)
			}
			if !strings.Contains(logs.String(), vendorReqID) ||
				!strings.Contains(logs.String(), `"upstream_kind":"dashscope"`) ||
				!strings.Contains(logs.String(), fmt.Sprintf(`"status":%d`, status)) {
				t.Fatalf("sanitized metadata missing from logs: %s", logs.String())
			}
		})
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHandler_TransportFailureIsSanitizedAndRefundable(t *testing.T) {
	const apiKey = "transport-secret-key"
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	client := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+apiKey {
			t.Errorf("Authorization = %q", got)
		}
		return nil, errors.New("dial failed without response body")
	})}
	models := atomic.Value{}
	models.Store([]string{"allowed"})
	cfg := config{
		upstreamURL: "http://upstream.invalid/v1/chat/completions", upstreamKind: upstreamOpenAI,
		upstreamAPIKey: apiKey, maxBodyBytes: defaultMaxBodyBytes, usageField: "total_tokens",
	}
	req := httptest.NewRequest(http.MethodPost, defaultEndpoint, strings.NewReader(`{"model":"allowed"}`))
	rec := httptest.NewRecorder()
	handleChatCompletionsWithConfig(rec, req, client, cfg, &models)

	if rec.Code != http.StatusBadGateway || rec.Header().Get(runnerErrorHeader) != "true" {
		t.Fatalf("status=%d runner-error=%q; want 502,true", rec.Code, rec.Header().Get(runnerErrorHeader))
	}
	combined := logs.String() + rec.Body.String()
	if strings.Contains(combined, apiKey) || strings.Contains(combined, "dial failed") {
		t.Fatalf("transport detail or secret leaked: %s", combined)
	}
	if !strings.Contains(logs.String(), `"status":502`) || !strings.Contains(logs.String(), `"upstream_kind":"openai"`) {
		t.Fatalf("status or upstream kind missing from logs: %s", logs.String())
	}
}

func TestHandler_WeightedAccountingMatchesAcrossModes(t *testing.T) {
	const weighted = uint64(72) // 12 prompt + 30 completion * 2
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "non-streaming", true: "streaming"}[stream], func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, vllmStreamFixture)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, ` { "usage": { "prompt_tokens": 12, "completion_tokens": 30, "total_tokens": 42 } } `)
			}))
			t.Cleanup(upstream.Close)
			handler := newConfiguredRunner(t, upstream.URL, func(cfg *config) {
				cfg.outputWeights = map[string]uint64{"allowed": 2}
			})
			body, _ := json.Marshal(map[string]any{"model": "allowed", "stream": stream})
			req := httptest.NewRequest(http.MethodPost, defaultEndpoint, bytes.NewReader(body))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if stream {
				if got := rec.Header().Get(workUnitsTrailer); got != "72" {
					t.Fatalf("streaming work units = %q; want 72", got)
				}
				return
			}
			if got := rec.Header().Get(workUnitsTrailer); got != "72" {
				t.Fatalf("non-streaming work units = %q; want 72", got)
			}
			const wantBody = ` { "usage": { "prompt_tokens": 12, "completion_tokens": 30, "total_tokens": 42 } } `
			if rec.Body.String() != wantBody {
				t.Fatalf("non-streaming body changed:\n got: %q\nwant: %q", rec.Body.String(), wantBody)
			}
		})
	}
}

func TestHandler_UnweightedModelRetainsUsageFieldBehavior(t *testing.T) {
	const responseBody = ` { "usage": { "prompt_tokens": 12, "completion_tokens": 30, "total_tokens": 42 } } `
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "non-streaming", true: "streaming"}[stream], func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, vllmStreamFixture)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, responseBody)
			}))
			t.Cleanup(upstream.Close)
			handler := newConfiguredRunner(t, upstream.URL, func(cfg *config) {
				cfg.usageField = "completion_tokens"
				cfg.outputWeights = map[string]uint64{"weighted-model": 9}
			})
			body, _ := json.Marshal(map[string]any{"model": "allowed", "stream": stream})
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, defaultEndpoint, bytes.NewReader(body)))

			if stream {
				if got := rec.Header().Get(workUnitsTrailer); got != "30" {
					t.Fatalf("streaming completion_tokens units = %q; want 30", got)
				}
				return
			}
			if got := rec.Header().Get(workUnitsTrailer); got != "" {
				t.Fatalf("unweighted non-streaming response gained work-units header %q", got)
			}
			if rec.Body.String() != responseBody {
				t.Fatalf("unweighted non-streaming body changed:\n got: %q\nwant: %q", rec.Body.String(), responseBody)
			}
		})
	}
}

func TestDiscoverModelsWithConfigFiltersAndAuthenticates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path = %q; want /v1/models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer operator-key" {
			t.Errorf("Authorization = %q", got)
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"first"},{"id":"model-b"},{"id":"last"},{"id":"model-a"}]}`)
	}))
	t.Cleanup(server.Close)
	ids, err := discoverModelsWithConfig(http.DefaultClient, server.URL, config{
		upstreamAPIKey: "operator-key",
		modelAllowlist: modelAllowlist{"model-a": {}, "model-b": {}, "not-discovered": {}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"model-b", "model-a"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("models = %v; want %v", ids, want)
	}
}

func TestDiscoverModelsWithUnsetAllowlistPreservesUpstreamModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"id":"second"},{"id":"first"}]}`)
	}))
	t.Cleanup(server.Close)

	ids, err := discoverModelsWithConfig(http.DefaultClient, server.URL, config{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"second", "first"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("models = %v; want %v", ids, want)
	}
}

func TestDiscoveryFailureLogsStatusAndRequestIDWithoutBodyOrSecret(t *testing.T) {
	const (
		apiKey      = "discovery-operator-secret"
		vendorBody  = "discovery-private-body"
		vendorReqID = "discovery-request-456"
	)
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+apiKey {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("X-Dashscope-Request-ID", vendorReqID)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, vendorBody)
	}))
	t.Cleanup(server.Close)

	_, err := discoverModelsWithRetryConfig(http.DefaultClient, server.URL, config{
		upstreamKind: upstreamDashScope, upstreamAPIKey: apiKey,
	}, 1, 0)
	if err == nil {
		t.Fatal("discovery should fail")
	}
	if strings.Contains(logs.String(), apiKey) || strings.Contains(logs.String(), vendorBody) {
		t.Fatalf("discovery secret or body leaked: %s", logs.String())
	}
	for _, want := range []string{vendorReqID, `"status":401`, `"upstream_kind":"dashscope"`} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("log missing %q: %s", want, logs.String())
		}
	}
}
