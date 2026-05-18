// Package testserver provides deterministic httptest servers for benchmark tests.
package testserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

// StreamStep describes one scripted Server-Sent Events write by the fake OpenAI server.
type StreamStep struct {
	// Delay is the optional duration to wait immediately before this step is emitted; zero means no step-specific delay.
	Delay time.Duration

	// Data is the raw SSE data payload, not including the "data: " prefix; empty emits an explicit empty data event when Comment is also empty.
	Data string

	// Comment is an optional SSE comment payload, not including the leading colon; empty means no comment is emitted for this step.
	Comment string
}

// OpenAIConfig configures a deterministic fake OpenAI-compatible Chat Completions server.
type OpenAIConfig struct {
	// Steps is the scripted SSE stream emitted for successful requests; nil or empty means the response body ends without events.
	Steps []StreamStep

	// DelayBeforeHeaders is the optional duration to wait before response headers are written; zero means no delay.
	DelayBeforeHeaders time.Duration

	// DelayBeforeFirstEvent is the optional duration to wait after headers before the first stream step; zero means no delay.
	DelayBeforeFirstEvent time.Duration

	// DelayBeforeFirstContent is the optional duration to wait before the first data step that appears to carry a content delta; zero means no delay.
	DelayBeforeFirstContent time.Duration

	// DelayBetweenSteps is the optional duration to wait before each stream step after the first; zero means no inter-step delay.
	DelayBetweenSteps time.Duration

	// StatusCode is the HTTP status code for requests that pass validation; zero means 200 OK.
	StatusCode int

	// ResponseBody is the non-stream response body written when StatusCode is not 2xx; empty means a default status body is used.
	ResponseBody string
}

// OpenAIRequest records one request observed by OpenAIServer.
type OpenAIRequest struct {
	// Path is the request URL path observed by the fake server; empty means no request was recorded.
	Path string

	// Model is the model identifier decoded from the JSON request body; empty means omitted or undecodable.
	Model string

	// Stream is the stream flag decoded from the JSON request body; false means omitted, false, or undecodable.
	Stream bool

	// AuthorizationPresent is true when the request included any Authorization header value; false means it was missing.
	AuthorizationPresent bool

	// RawBody is the raw JSON request body bytes observed by the fake server; values may contain sensitive prompt text.
	RawBody []byte
}

// OpenAIServer is a deterministic fake OpenAI-compatible Chat Completions server.
type OpenAIServer struct {
	server   *httptest.Server
	config   OpenAIConfig
	mu       sync.Mutex
	requests []OpenAIRequest
}

// NewOpenAIServer starts a fake OpenAI-compatible Chat Completions server.
func NewOpenAIServer(config OpenAIConfig) *OpenAIServer {
	fake := &OpenAIServer{config: config}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.handle))
	return fake
}

// URL returns the fake server base URL without a trailing slash.
func (s *OpenAIServer) URL() string {
	return s.server.URL
}

// Close shuts down the fake server and releases its listener.
func (s *OpenAIServer) Close() {
	s.server.Close()
}

// Requests returns a snapshot of requests observed by the fake server.
func (s *OpenAIServer) Requests() []OpenAIRequest {
	s.mu.Lock()
	defer s.mu.Unlock()

	requests := make([]OpenAIRequest, len(s.requests))
	copy(requests, s.requests)
	for index := range requests {
		requests[index].RawBody = append([]byte(nil), requests[index].RawBody...)
	}

	return requests
}

func (s *OpenAIServer) handle(w http.ResponseWriter, r *http.Request) {
	if s.config.DelayBeforeHeaders > 0 {
		time.Sleep(s.config.DelayBeforeHeaders)
	}

	if r.URL.Path != "/chat/completions" && r.URL.Path != "/v1/chat/completions" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method must be POST", http.StatusMethodNotAllowed)
		return
	}

	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read request body", http.StatusBadRequest)
		return
	}
	requestBody := struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}{}
	if err := json.Unmarshal(rawBody, &requestBody); err != nil {
		http.Error(w, "decode request body", http.StatusBadRequest)
		return
	}

	observed := OpenAIRequest{
		Path:                 r.URL.Path,
		Model:                requestBody.Model,
		Stream:               requestBody.Stream,
		AuthorizationPresent: r.Header.Get("Authorization") != "",
		RawBody:              append([]byte(nil), rawBody...),
	}
	s.record(observed)

	if !observed.AuthorizationPresent {
		http.Error(w, "authorization header is required", http.StatusUnauthorized)
		return
	}
	if !observed.Stream {
		http.Error(w, "stream must be true", http.StatusBadRequest)
		return
	}

	statusCode := s.config.StatusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		body := s.config.ResponseBody
		if body == "" {
			body = fmt.Sprintf("status %d", statusCode)
		}
		http.Error(w, body, statusCode)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(statusCode)
	flush(w)
	s.emitStream(w)
}

func (s *OpenAIServer) record(request OpenAIRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.requests = append(s.requests, request)
}

func (s *OpenAIServer) emitStream(w http.ResponseWriter) {
	if s.config.DelayBeforeFirstEvent > 0 && len(s.config.Steps) > 0 {
		time.Sleep(s.config.DelayBeforeFirstEvent)
	}

	firstContentDelayed := false
	for index, step := range s.config.Steps {
		if index > 0 && s.config.DelayBetweenSteps > 0 {
			time.Sleep(s.config.DelayBetweenSteps)
		}
		if step.Delay > 0 {
			time.Sleep(step.Delay)
		}
		if !firstContentDelayed && isContentStep(step) {
			firstContentDelayed = true
			if s.config.DelayBeforeFirstContent > 0 {
				time.Sleep(s.config.DelayBeforeFirstContent)
			}
		}
		emitStep(w, step)
		flush(w)
	}
}

func emitStep(w http.ResponseWriter, step StreamStep) {
	if step.Comment != "" {
		comment := step.Comment
		if strings.HasPrefix(comment, ":") {
			writeResponse(w, "%s\n\n", comment)
		} else {
			writeResponse(w, ": %s\n\n", comment)
		}
	}
	if step.Data != "" || step.Comment == "" {
		writeResponse(w, "data: %s\n\n", step.Data)
	}
}

func isContentStep(step StreamStep) bool {
	return strings.Contains(step.Data, `"content"`)
}

func flush(w http.ResponseWriter) {
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func writeResponse(w http.ResponseWriter, format string, args ...any) {
	//nolint:gosec // Fake server call sites pass constant format strings; streamed payloads are arguments.
	if _, err := fmt.Fprintf(w, format, args...); err != nil {
		return
	}
}
