// Package mockvendor provides a deterministic authenticated
// OpenAI-compatible upstream for runner integration tests.
package mockvendor

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
)

const (
	AllowedModel = "vendor-visible"
	HiddenModel  = "vendor-hidden"
	PromptTokens = uint64(5)
	OutputTokens = uint64(7)
	TotalTokens  = uint64(12)
)

// Request records what reached the vendor. Authorization is retained only in
// this test fixture so an integration test can distinguish runner-supplied
// operator authentication from client-supplied authentication.
type Request struct {
	Method        string
	Path          string
	Authorization string
	Body          string
}

// Server is an authenticated mock vendor and its concurrency-safe request log.
type Server struct {
	HTTP *httptest.Server

	key      string
	mu       sync.Mutex
	requests []Request
}

// New starts a deterministic mock which accepts exactly Bearer <key>.
func New(key string) *Server {
	s := &Server{key: key}
	s.HTTP = httptest.NewServer(http.HandlerFunc(s.serveHTTP))
	return s
}

func (s *Server) Close() {
	s.HTTP.Close()
}

// Requests returns a snapshot of all requests, including rejected attempts.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.requests...)
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.requests = append(s.requests, Request{
		Method:        r.Method,
		Path:          r.URL.Path,
		Authorization: r.Header.Get("Authorization"),
		Body:          string(body),
	})
	s.mu.Unlock()

	if r.Header.Get("Authorization") != "Bearer "+s.key {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"invalid vendor bearer","type":"authentication_error"}}`)
		return
	}

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"vendor-visible","object":"model"},{"id":"vendor-hidden","object":"model"}]}`)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/chat/completions":
		s.chatCompletion(w, body)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) chatCompletion(w http.ResponseWriter, body []byte) {
	var request struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if request.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, event := range []string{
			"data: {\"id\":\"mock-stream\",\"object\":\"chat.completion.chunk\",\"model\":\"vendor-visible\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\n",
			"data: {\"id\":\"mock-stream\",\"object\":\"chat.completion.chunk\",\"model\":\"vendor-visible\",\"choices\":[],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":7,\"total_tokens\":12}}\n\n",
			"data: [DONE]\n\n",
		} {
			_, _ = io.WriteString(w, event)
			if flusher != nil {
				flusher.Flush()
			}
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"id":"mock-response","object":"chat.completion","model":"vendor-visible","choices":[{"index":0,"message":{"role":"assistant","content":"hello"}}],"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}}`)
}
