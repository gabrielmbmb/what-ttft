package groq

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

// TestChatCompletionsCapturesTimingReasoningAndUsage verifies the default chat path parses reasoning,
// usage, and Groq's x_groq server timing.
func TestChatCompletionsCapturesTimingReasoningAndUsage(t *testing.T) {
	const apiKey = "gsk_test-secret"

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
		writeSSE(t, w, `{"service_tier":"on_demand","choices":[{"index":0,"delta":{"role":"assistant"}}]}`)
		writeSSE(t, w, `{"choices":[{"index":0,"delta":{"reasoning":"thinking"}}]}`)
		writeSSE(t, w, `{"choices":[{"index":0,"delta":{"content":"Hello"}}]}`)
		writeSSE(t, w, `{"choices":[{"index":0,"delta":{"content":" world"},"finish_reason":"stop"}]}`)
		writeSSE(t, w, `{"choices":[],"x_groq":{"id":"req_1","usage":{"prompt_tokens":11,"completion_tokens":2,"total_tokens":13,"queue_time":0.02,"prompt_time":0.006,"completion_time":0.014,"total_time":0.09}}}`)
		writeSSE(t, w, "[DONE]")
	}))
	defer server.Close()

	provider := New(Config{BaseURL: server.URL, APIKey: apiKey, Model: "llama-3.3-70b-versatile", ServiceTier: ServiceTierOnDemand})
	obs := newTestObserver()

	err := provider.StreamChat(context.Background(), whatttft.ProviderRequest{
		RequestID: "req-1",
		Scenario:  whatttft.Scenario{MaxOutputTokens: 64},
		Prompt:    "Say hello.",
	}, obs)
	if err != nil {
		t.Fatalf("stream chat: %v", err)
	}
	assertNoHandlerErrors(t, handlerErrors)
	if !sawAuth.Load() {
		t.Fatal("authorization header was not observed")
	}

	requestBody := <-requestCh
	if !requestBody.Stream || requestBody.MaxCompletionTokens == nil || *requestBody.MaxCompletionTokens != 64 {
		t.Fatalf("request body unexpected: %+v", requestBody)
	}
	if requestBody.ServiceTier != ServiceTierOnDemand {
		t.Fatalf("service_tier = %q, want on_demand", requestBody.ServiceTier)
	}

	if visible := obs.visibleOutput(); len(visible) != 2 || visible[0].Text != "Hello" || visible[1].Text != " world" {
		t.Fatalf("visible output = %+v, want Hello/ world", visible)
	}
	if reasoning := obs.reasoningOutput(); len(reasoning) != 1 || reasoning[0].Text != "thinking" || reasoning[0].Visible {
		t.Fatalf("reasoning output = %+v, want one non-visible 'thinking'", reasoning)
	}
	usages := obs.usages()
	if len(usages) != 1 || usages[0].CompletionTokens == nil || *usages[0].CompletionTokens != 2 {
		t.Fatalf("usage = %+v, want completion_tokens 2", usages)
	}

	timing := obs.latestHTTP().ServerTiming
	if timing == nil {
		t.Fatal("server timing should be captured from x_groq.usage")
	}
	if timing.TotalTimeMS == nil || *timing.TotalTimeMS != 90 {
		t.Fatalf("total_time_ms = %v, want 90 (0.09s)", timing.TotalTimeMS)
	}
	if timing.QueueTimeMS == nil || *timing.QueueTimeMS != 20 {
		t.Fatalf("queue_time_ms = %v, want 20 (0.02s)", timing.QueueTimeMS)
	}
	if obs.latestHTTP().ObservedServiceTier != "on_demand" {
		t.Fatalf("observed service tier = %q, want on_demand", obs.latestHTTP().ObservedServiceTier)
	}
}

// TestChatCompletionsCapturesTimingFromTopLevelUsage verifies timing is also read from a top-level usage payload.
func TestChatCompletionsCapturesTimingFromTopLevelUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, w, `{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`)
		writeSSE(t, w, `{"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6,"completion_time":0.01}}`)
		writeSSE(t, w, "[DONE]")
	}))
	defer server.Close()

	provider := New(Config{BaseURL: server.URL, APIKey: "k", Model: "m", IncludeUsage: true})
	obs := newTestObserver()
	if err := provider.StreamChat(context.Background(), whatttft.ProviderRequest{RequestID: "req-1", Prompt: "hi"}, obs); err != nil {
		t.Fatalf("stream chat: %v", err)
	}
	timing := obs.latestHTTP().ServerTiming
	if timing == nil || timing.CompletionTimeMS == nil || *timing.CompletionTimeMS != 10 {
		t.Fatalf("completion_time_ms = %v, want 10 (0.01s)", timing)
	}
}

// TestResponsesAPIParsesStreamingEvents verifies the beta Responses path.
func TestResponsesAPIParsesStreamingEvents(t *testing.T) {
	requestCh := make(chan responseRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			http.NotFound(w, r)
			return
		}
		var reqBody responseRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		requestCh <- reqBody
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, w, `{"type":"response.output_text.delta","delta":"Hello"}`)
		writeSSE(t, w, `{"type":"response.output_text.delta","delta":" world"}`)
		writeSSE(t, w, `{"type":"response.completed","response":{"status":"completed","service_tier":"auto","usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}`)
	}))
	defer server.Close()

	provider := New(Config{API: ResponsesAPI, BaseURL: server.URL, APIKey: "k", Model: "m", ServiceTier: ServiceTierAuto})
	obs := newTestObserver()
	err := provider.StreamChat(context.Background(), whatttft.ProviderRequest{
		RequestID: "req-1",
		Scenario:  whatttft.Scenario{SystemPrompt: "be concise", MaxOutputTokens: 32},
		Prompt:    "Say hello.",
	}, obs)
	if err != nil {
		t.Fatalf("stream responses: %v", err)
	}

	requestBody := <-requestCh
	if requestBody.Input != "Say hello." || requestBody.Instructions != "be concise" || !requestBody.Stream {
		t.Fatalf("responses request unexpected: %+v", requestBody)
	}
	if visible := obs.visibleOutput(); len(visible) != 2 || visible[0].Text != "Hello" || visible[1].Text != " world" {
		t.Fatalf("visible output = %+v, want Hello/ world", visible)
	}
	usages := obs.usages()
	if len(usages) != 1 || usages[0].CompletionTokens == nil || *usages[0].CompletionTokens != 2 {
		t.Fatalf("usage = %+v, want completion_tokens 2", usages)
	}
	if obs.latestHTTP().ObservedServiceTier != "auto" {
		t.Fatalf("observed service tier = %q, want auto", obs.latestHTTP().ObservedServiceTier)
	}
}

// TestStreamChatRedactsNon200Body verifies error bodies never leak the API key.
func TestStreamChatRedactsNon200Body(t *testing.T) {
	const apiKey = "gsk_super-secret"
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
	if err == nil || strings.Contains(err.Error(), apiKey) || !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("error = %v, want redacted non-200", err)
	}
	if obs.latestHTTP().StatusCode != http.StatusUnauthorized {
		t.Fatalf("status code = %d, want 401", obs.latestHTTP().StatusCode)
	}
}

// TestStreamChatRejectsMalformedJSON verifies malformed chat chunks surface a decode error.
func TestStreamChatRejectsMalformedJSON(t *testing.T) {
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

// TestCapabilitiesAndValidation verifies capabilities and required-input validation.
func TestCapabilitiesAndValidation(t *testing.T) {
	if New(Config{Model: "m"}).Name() != providerName {
		t.Fatal("name should be groq")
	}
	if !New(Config{Model: "m", API: ResponsesAPI}).Capabilities().SupportsUsageInStream {
		t.Fatal("responses api should support usage in stream")
	}
	if New(Config{Model: "m"}).Capabilities().SupportsUsageInStream {
		t.Fatal("chat api without IncludeUsage should not claim usage in stream")
	}

	if err := New(Config{APIKey: "k"}).StreamChat(context.Background(), whatttft.ProviderRequest{}, newTestObserver()); err == nil || !strings.Contains(err.Error(), "model is required") {
		t.Fatalf("missing model error = %v", err)
	}
	if err := New(Config{Model: "m"}).StreamChat(context.Background(), whatttft.ProviderRequest{}, newTestObserver()); err == nil || !strings.Contains(err.Error(), "API key is required") {
		t.Fatalf("missing key error = %v", err)
	}
	if err := New(Config{Model: "m", APIKey: "k", ServiceTier: "priority"}).StreamChat(context.Background(), whatttft.ProviderRequest{}, newTestObserver()); err == nil || !strings.Contains(err.Error(), "unsupported groq service tier") {
		t.Fatalf("bad service tier error = %v", err)
	}
	if err := New(Config{Model: "m", APIKey: "k", API: "bogus"}).StreamChat(context.Background(), whatttft.ProviderRequest{}, newTestObserver()); err == nil || !strings.Contains(err.Error(), "unsupported groq API") {
		t.Fatalf("bad api error = %v", err)
	}
	if err := New(Config{Model: "m", APIKey: "k"}).StreamChat(context.Background(), whatttft.ProviderRequest{}, nil); err == nil || !strings.Contains(err.Error(), "observer is nil") {
		t.Fatalf("nil observer error = %v", err)
	}
}

func writeSSE(t *testing.T, w http.ResponseWriter, data string) {
	t.Helper()

	if _, err := w.Write([]byte("data: " + data + "\n\n")); err != nil {
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
