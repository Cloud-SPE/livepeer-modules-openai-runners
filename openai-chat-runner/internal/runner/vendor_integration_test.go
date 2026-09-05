package runner

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Cloud-SPE/livepeer-modules-openai-runners/openai-tester/mockvendor"
)

func TestVendorPhaseZeroIntegration(t *testing.T) {
	const (
		operatorKey = "operator-owned-secret"
		clientKey   = "client-supplied-secret"
		weight      = uint64(3)
		wantUnits   = mockvendor.PromptTokens + mockvendor.OutputTokens*weight
	)

	vendor := mockvendor.New(operatorKey)
	t.Cleanup(vendor.Close)

	// The fixture itself enforces authentication rather than merely recording
	// the header for a later assertion.
	for name, authorization := range map[string]string{
		"missing bearer": "",
		"wrong bearer":   "Bearer wrong-secret",
	} {
		t.Run(name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, vendor.HTTP.URL+"/v1/models", nil)
			if err != nil {
				t.Fatal(err)
			}
			if authorization != "" {
				req.Header.Set("Authorization", authorization)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("mock status = %d; want 401", resp.StatusCode)
			}
		})
	}

	// Load every Phase 0 vendor knob through the same startup path as Run.
	t.Setenv("UPSTREAM_URL", vendor.HTTP.URL+defaultEndpoint)
	t.Setenv("UPSTREAM_KIND", "openai")
	t.Setenv("UPSTREAM_API_KEY", "  "+operatorKey+"  ")
	t.Setenv("MODEL_ALLOWLIST", " "+mockvendor.AllowedModel+" ")
	t.Setenv("OUTPUT_TOKEN_WEIGHT", mockvendor.AllowedModel+"=3")
	t.Setenv("CAPABILITY_NAME", defaultCapability)
	t.Setenv("USAGE_FIELD", "completion_tokens")
	for _, name := range []string{
		"SERVED_MODEL_NAME", "BACKEND_MODEL", "CONTEXT_LENGTH", "REASONING_PARSER", "TOOL_CALL_PARSER", "QUANTIZATION",
	} {
		t.Setenv(name, "")
	}
	cfg, err := configFromEnv()
	if err != nil {
		t.Fatal(err)
	}

	models, err := discoverModelsWithConfig(http.DefaultClient, upstreamBase(cfg.upstreamURL), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(models, []string{mockvendor.AllowedModel}) {
		t.Fatalf("discovered models = %v; want only allowed model", models)
	}

	discovered := atomic.Value{}
	discovered.Store(models)
	mux := http.NewServeMux()
	mux.HandleFunc(defaultEndpoint, func(w http.ResponseWriter, r *http.Request) {
		handleChatCompletionsWithConfig(w, r, http.DefaultClient, cfg, &discovered)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		loaded, _ := loadModels(&discovered)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "models": loaded})
	})
	mux.HandleFunc(contractPath, func(w http.ResponseWriter, r *http.Request) {
		handleContract(w, r, cfg, &discovered)
	})
	mux.HandleFunc(modelsPath, func(w http.ResponseWriter, r *http.Request) {
		handleModels(w, r, cfg, &discovered)
	})
	proxy := httptest.NewServer(mux)
	t.Cleanup(proxy.Close)

	assertModels := func(path, wantUpstreamKind string) {
		t.Helper()
		resp, err := http.Get(proxy.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		var payload struct {
			Models       []string `json:"models"`
			UpstreamKind string   `json:"upstream_kind"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(payload.Models, []string{mockvendor.AllowedModel}) {
			t.Fatalf("%s models = %v; hidden model was not filtered", path, payload.Models)
		}
		if payload.UpstreamKind != wantUpstreamKind {
			t.Fatalf("%s upstream_kind = %q; want %q", path, payload.UpstreamKind, wantUpstreamKind)
		}
	}
	assertModels("/healthz", "")

	// The contract advertises exactly the allowlisted model, with the
	// vendor kind as identity.provider.
	contractResp, err := http.Get(proxy.URL + contractPath)
	if err != nil {
		t.Fatal(err)
	}
	var entry struct {
		CapabilityID string            `json:"capability_id"`
		Identity     map[string]string `json:"identity"`
	}
	if err := json.NewDecoder(contractResp.Body).Decode(&entry); err != nil {
		t.Fatalf("contract should be a single entry for one allowlisted model: %v", err)
	}
	_ = contractResp.Body.Close()
	if entry.CapabilityID != cfg.capability || entry.Identity["openai.model"] != mockvendor.AllowedModel || entry.Identity["provider"] != "openai" {
		t.Fatalf("contract entry = %+v", entry)
	}

	modelsResp, err := http.Get(proxy.URL + modelsPath)
	if err != nil {
		t.Fatal(err)
	}
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(modelsResp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	_ = modelsResp.Body.Close()
	if len(list.Data) != 1 || list.Data[0].ID != mockvendor.AllowedModel {
		t.Fatalf("/v1/models = %+v; hidden model was not filtered", list.Data)
	}

	chatCallsBefore := countVendorChatCalls(vendor.Requests())
	hidden := doChatRequest(t, proxy.URL, clientKey, mockvendor.HiddenModel, false)
	_, _ = io.Copy(io.Discard, hidden.Body)
	_ = hidden.Body.Close()
	if hidden.StatusCode != http.StatusBadRequest {
		t.Fatalf("hidden-model status = %d; want 400", hidden.StatusCode)
	}
	if got := countVendorChatCalls(vendor.Requests()); got != chatCallsBefore {
		t.Fatalf("hidden-model request reached vendor: chat calls %d -> %d", chatCallsBefore, got)
	}

	for _, stream := range []bool{false, true} {
		resp := doChatRequest(t, proxy.URL, clientKey, mockvendor.AllowedModel, stream)
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("stream=%v status = %d, body=%s", stream, resp.StatusCode, body)
		}
		if stream && !bytes.Contains(body, []byte("data: [DONE]")) {
			t.Fatalf("streaming fixture was not forwarded: %s", body)
		}
		if got := resp.Header.Get(workUnitsHeader); !stream && got != "26" {
			t.Fatalf("non-streaming work units = %q; want %d", got, wantUnits)
		}
		if got := resp.Trailer.Get(workUnitsHeader); stream && got != "26" {
			t.Fatalf("streaming work units trailer = %q; want %d (headers=%v trailers=%v body=%s)", got, wantUnits, resp.Header, resp.Trailer, body)
		}
	}

	requests := vendor.Requests()
	if got := countVendorChatCalls(requests); got != 2 {
		t.Fatalf("vendor chat calls = %d; want streaming and non-streaming only", got)
	}
	for _, request := range requests[2:] { // Skip the two deliberate auth rejections.
		if request.Authorization != "Bearer "+operatorKey {
			t.Errorf("%s %s authorization = %q; operator bearer was not supplied", request.Method, request.Path, request.Authorization)
		}
		if strings.Contains(request.Authorization, clientKey) {
			t.Errorf("client authorization reached vendor: %q", request.Authorization)
		}
	}
}

func doChatRequest(t *testing.T, baseURL, clientKey, model string, stream bool) *http.Response {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"model": model, "stream": stream,
		"messages": []map[string]string{{"role": "user", "content": "hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+defaultEndpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+clientKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func countVendorChatCalls(requests []mockvendor.Request) int {
	count := 0
	for _, request := range requests {
		if request.Path == defaultEndpoint {
			count++
		}
	}
	return count
}
