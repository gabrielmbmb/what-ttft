package together

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

// TestProviderStreamChatParsesStreamingEvents verifies the happy-path Together stream.
func TestProviderStreamChatParsesStreamingEvents(t *testing.T) {
	const apiKey = "test-secret"

	requestCh := make(chan chatCompletionRequest, 1)
	handlerErrors := make(chan string, 8)
	var sawAuth atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			handlerErrors <- fmt.Sprintf("path = %q, want /chat/completions", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got == "Bearer "+apiKey {
			sawAuth.Store(true)
		} else {
			handlerErrors <- fmt.Sprintf("authorization header = %q", got)
		}

		var reqBody chatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			handlerErrors <- fmt.Sprintf("decode request body: %v", err)
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		requestCh <- reqBody

		w.Header().Set("Content-Type", "text/event-stream")
		writeRaw(t, w, ": together heartbeat\n\n")
		writeSSE(t, w, "")
		writeSSE(t, w, `{"choices":[{"index":0,"delta":{"role":"assistant"}}]}`)
		writeSSE(t, w, `{"choices":[{"index":0,"delta":{"reasoning":"let me think"}}]}`)
		writeSSE(t, w, `{"choices":[{"index":0,"delta":{"content":"Hello"}}]}`)
		writeSSE(t, w, `{"choices":[{"index":0,"delta":{"content":" world"},"finish_reason":"stop"}]}`)
		writeSSE(t, w, `{"choices":[],"usage":{"prompt_tokens":41,"completion_tokens":2,"total_tokens":43}}`)
		writeSSE(t, w, "[DONE]")
	}))
	defer server.Close()

	temperature := 0.0
	provider := New(Config{
		BaseURL:      server.URL,
		APIKey:       apiKey,
		Model:        "meta-llama/Llama-3.3-70B-Instruct-Turbo",
		IncludeUsage: true,
	})
	obs := newTestObserver()

	err := provider.StreamChat(context.Background(), whatttft.ProviderRequest{
		RequestID: "req-1",
		Scenario: whatttft.Scenario{
			SystemPrompt:    "You are concise.",
			MaxOutputTokens: 64,
			Temperature:     &temperature,
		},
		Prompt: "Say hello.",
	}, obs)
	if err != nil {
		t.Fatalf("stream chat: %v", err)
	}
	assertNoHandlerErrors(t, handlerErrors)
	if !sawAuth.Load() {
		t.Fatal("authorization header was not observed")
	}

	requestBody := <-requestCh
	if !requestBody.Stream {
		t.Fatal("stream should be true")
	}
	if requestBody.MaxCompletionTokens == nil || *requestBody.MaxCompletionTokens != 64 {
		t.Fatalf("max_completion_tokens = %v, want 64", requestBody.MaxCompletionTokens)
	}
	if requestBody.StreamOptions == nil || !requestBody.StreamOptions.IncludeUsage {
		t.Fatalf("stream_options.include_usage should be true, got %+v", requestBody.StreamOptions)
	}

	visible := obs.visibleOutput()
	if len(visible) != 2 || visible[0].Text != "Hello" || visible[1].Text != " world" {
		t.Fatalf("visible output = %+v, want Hello/ world", visible)
	}
	if obs.eventCount(whatttft.EventFirstOutputDelta) != 1 {
		t.Fatalf("first output delta marks = %d, want 1", obs.eventCount(whatttft.EventFirstOutputDelta))
	}
	if obs.eventCount(whatttft.EventBodyEOF) != 1 {
		t.Fatalf("body eof marks = %d, want 1", obs.eventCount(whatttft.EventBodyEOF))
	}

	reasoning := obs.reasoningOutput()
	if len(reasoning) != 1 || reasoning[0].Text != "let me think" || reasoning[0].Visible {
		t.Fatalf("reasoning output = %+v, want one non-visible 'let me think'", reasoning)
	}

	usages := obs.usages()
	if len(usages) != 1 || usages[0].CompletionTokens == nil || *usages[0].CompletionTokens != 2 {
		t.Fatalf("usage = %+v, want completion_tokens 2", usages)
	}
}

// TestProviderStreamChatCapturesUsageWithoutIncludeUsage verifies usage is parsed even when include_usage is off.
func TestProviderStreamChatCapturesUsageWithoutIncludeUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody chatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		if reqBody.StreamOptions != nil {
			t.Errorf("stream_options should be omitted, got %+v", reqBody.StreamOptions)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, w, `{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`)
		writeSSE(t, w, `{"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`)
		writeSSE(t, w, "[DONE]")
	}))
	defer server.Close()

	provider := New(Config{BaseURL: server.URL, APIKey: "k", Model: "m"})
	obs := newTestObserver()
	if err := provider.StreamChat(context.Background(), whatttft.ProviderRequest{RequestID: "req-1", Prompt: "hi"}, obs); err != nil {
		t.Fatalf("stream chat: %v", err)
	}
	usages := obs.usages()
	if len(usages) != 1 || usages[0].TotalTokens == nil || *usages[0].TotalTokens != 6 {
		t.Fatalf("usage = %+v, want total_tokens 6 without include_usage", usages)
	}
}

// TestProviderStreamChatRedactsNon200Body verifies error bodies never leak the API key.
func TestProviderStreamChatRedactsNon200Body(t *testing.T) {
	const apiKey = "super-secret-key"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		if _, err := fmt.Fprintf(w, "invalid key %s", apiKey); err != nil {
			t.Errorf("write error body: %v", err)
		}
	}))
	defer server.Close()

	provider := New(Config{BaseURL: server.URL, APIKey: apiKey, Model: "m"})
	obs := newTestObserver()
	err := provider.StreamChat(context.Background(), whatttft.ProviderRequest{RequestID: "req-1", Prompt: "hi"}, obs)
	if err == nil {
		t.Fatal("expected an error for non-200 response")
	}
	if strings.Contains(err.Error(), apiKey) {
		t.Fatalf("error leaked API key: %v", err)
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("error missing redaction marker: %v", err)
	}
	if obs.latestHTTP().StatusCode != http.StatusUnauthorized {
		t.Fatalf("status code = %d, want 401", obs.latestHTTP().StatusCode)
	}
}

// TestProviderStreamChatRejectsMalformedJSON verifies malformed chunks surface a decode error.
func TestProviderStreamChatRejectsMalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, w, `{"choices":[{"index":0,`)
	}))
	defer server.Close()

	provider := New(Config{BaseURL: server.URL, APIKey: "k", Model: "m"})
	obs := newTestObserver()
	err := provider.StreamChat(context.Background(), whatttft.ProviderRequest{RequestID: "req-1", Prompt: "hi"}, obs)
	if err == nil || !strings.Contains(err.Error(), "decode chat completion chunk") {
		t.Fatalf("error = %v, want decode chat completion chunk", err)
	}
}

// TestProviderCapabilitiesAndValidation verifies capabilities and required-input validation.
func TestProviderCapabilitiesAndValidation(t *testing.T) {
	caps := New(Config{Model: "m"}).Capabilities()
	if caps.StreamingProtocol != streamProtocolSSE || !caps.SupportsChat || !caps.SupportsUsageInStream {
		t.Fatalf("capabilities = %+v, unexpected", caps)
	}
	if caps.SupportsPromptCache {
		t.Fatal("together does not expose prompt cache")
	}
	if New(Config{Model: "m"}).Name() != providerName {
		t.Fatal("name should be together")
	}

	if err := New(Config{APIKey: "k"}).StreamChat(context.Background(), whatttft.ProviderRequest{}, newTestObserver()); err == nil || !strings.Contains(err.Error(), "model is required") {
		t.Fatalf("missing model error = %v", err)
	}
	if err := New(Config{Model: "m"}).StreamChat(context.Background(), whatttft.ProviderRequest{}, newTestObserver()); err == nil || !strings.Contains(err.Error(), "API key is required") {
		t.Fatalf("missing key error = %v", err)
	}
	if err := New(Config{Model: "m", APIKey: "k"}).StreamChat(context.Background(), whatttft.ProviderRequest{}, nil); err == nil || !strings.Contains(err.Error(), "observer is nil") {
		t.Fatalf("nil observer error = %v", err)
	}
}

func writeSSE(t *testing.T, w http.ResponseWriter, data string) {
	t.Helper()
	writeRaw(t, w, "data: "+data+"\n\n")
}

func writeRaw(t *testing.T, w http.ResponseWriter, data string) {
	t.Helper()

	if _, err := w.Write([]byte(data)); err != nil {
		t.Errorf("write stream data: %v", err)
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func assertNoHandlerErrors(t *testing.T, handlerErrors chan string) {
	t.Helper()

	for {
		select {
		case msg := <-handlerErrors:
			t.Errorf("handler error: %s", msg)
		default:
			return
		}
	}
}

type testObserver struct {
	mu       sync.Mutex
	events   map[whatttft.EventName]int
	outputs  []whatttft.OutputDelta
	usage    []whatttft.ProviderUsage
	httpRecs []whatttft.HTTPRecord
}

func newTestObserver() *testObserver {
	return &testObserver{events: make(map[whatttft.EventName]int)}
}

func (o *testObserver) Mark(name whatttft.EventName) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.events[name]++
}

func (o *testObserver) MarkFirst(name whatttft.EventName) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.events[name] == 0 {
		o.events[name] = 1
	}
}

func (o *testObserver) MarkLast(name whatttft.EventName) {
	o.Mark(name)
}

func (o *testObserver) OnStreamEvent(whatttft.StreamEvent) {}

func (o *testObserver) OnOutputDelta(delta whatttft.OutputDelta) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.outputs = append(o.outputs, delta)
}

func (o *testObserver) OnToken(whatttft.TokenEvent) {}

func (o *testObserver) OnUsage(usage whatttft.ProviderUsage) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.usage = append(o.usage, usage)
}

func (o *testObserver) OnCache(whatttft.CacheRecord) {}

func (o *testObserver) OnFinish(whatttft.FinishEvent) {}

func (o *testObserver) OnHTTP(record whatttft.HTTPRecord) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.httpRecs = append(o.httpRecs, record)
}

func (o *testObserver) eventCount(name whatttft.EventName) int {
	o.mu.Lock()
	defer o.mu.Unlock()

	return o.events[name]
}

func (o *testObserver) visibleOutput() []whatttft.OutputDelta {
	return o.filterOutput(true, textModality)
}

func (o *testObserver) reasoningOutput() []whatttft.OutputDelta {
	return o.filterOutput(false, reasoningModality)
}

func (o *testObserver) filterOutput(visible bool, modality string) []whatttft.OutputDelta {
	o.mu.Lock()
	defer o.mu.Unlock()

	filtered := make([]whatttft.OutputDelta, 0, len(o.outputs))
	for _, output := range o.outputs {
		if output.Visible == visible && output.Modality == modality && output.Text != "" {
			filtered = append(filtered, output)
		}
	}

	return filtered
}

func (o *testObserver) usages() []whatttft.ProviderUsage {
	o.mu.Lock()
	defer o.mu.Unlock()

	return append([]whatttft.ProviderUsage(nil), o.usage...)
}

func (o *testObserver) latestHTTP() whatttft.HTTPRecord {
	o.mu.Lock()
	defer o.mu.Unlock()

	if len(o.httpRecs) == 0 {
		return whatttft.HTTPRecord{}
	}

	return o.httpRecs[len(o.httpRecs)-1]
}
