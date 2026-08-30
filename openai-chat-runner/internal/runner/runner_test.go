package runner

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"x","usage":{"total_tokens":17}}`)
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
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"total_tokens":17`)) {
		t.Fatalf("response body missing forwarded JSON: %s", rec.Body.String())
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
				_, _ = io.WriteString(w, `{"usage":{"prompt_tokens":12,"completion_tokens":30,"total_tokens":42}}`)
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
			var response struct {
				Usage usageFields `json:"usage"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Usage.TotalTokens != weighted {
				t.Fatalf("non-streaming total_tokens = %d; want %d", response.Usage.TotalTokens, weighted)
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
		_, _ = io.WriteString(w, `{"data":[{"id":"first"},{"id":"allowed"},{"id":"last"}]}`)
	}))
	t.Cleanup(server.Close)
	ids, err := discoverModelsWithConfig(http.DefaultClient, server.URL, config{
		upstreamAPIKey: "operator-key",
		modelAllowlist: map[string]struct{}{"allowed": {}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "allowed" {
		t.Fatalf("models = %v; want [allowed]", ids)
	}
}
