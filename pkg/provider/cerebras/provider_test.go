package cerebras

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

// TestProviderStreamChatParsesStreamingEvents verifies the happy-path Cerebras stream.
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
		writeRaw(t, w, ": cerebras heartbeat\n\n")
		writeSSE(t, w, "")
		writeSSE(t, w, `{"choices":[{"index":0,"delta":{"role":"assistant"}}]}`)
		writeSSE(t, w, `{"choices":[{"index":0,"delta":{"reasoning":"thinking hard"}}]}`)
		writeSSE(t, w, `{"service_tier":"default","choices":[{"index":0,"delta":{"content":"Hello"}}]}`)
		writeSSE(t, w, `{"choices":[{"index":0,"delta":{"content":" world"},"finish_reason":"stop"}]}`)
		writeSSE(t, w, `{"choices":[],"usage":{"prompt_tokens":69,"completion_tokens":45,"total_tokens":114,"image_tokens":0,"prompt_tokens_details":{"cached_tokens":32},"completion_tokens_details":{"reasoning_tokens":8}},"time_info":{"queue_time":0.001,"prompt_time":0.006,"completion_time":0.014,"total_time":0.09,"created":1782514735.28}}`)
		writeSSE(t, w, "[DONE]")
	}))
	defer server.Close()

	temperature := 0.0
	seed := int64(7)
	provider := New(Config{
		BaseURL:      server.URL,
		APIKey:       apiKey,
		Model:        "gpt-oss-120b",
		ServiceTier:  ServiceTierDefault,
		IncludeUsage: true,
	})
	obs := newTestObserver()

	err := provider.StreamChat(context.Background(), whatttft.ProviderRequest{
		RequestID: "req-1",
		Scenario: whatttft.Scenario{
			SystemPrompt:    "You are concise.",
			MaxOutputTokens: 256,
			Temperature:     &temperature,
			Seed:            &seed,
			ReasoningEffort: "low",
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
	assertRequest(t, requestBody)
	assertObservation(t, obs)
}

func assertRequest(t *testing.T, req chatCompletionRequest) {
	t.Helper()

	if !req.Stream {
		t.Fatal("stream should be true")
	}
	if len(req.Messages) != 2 || req.Messages[0].Role != "system" || req.Messages[1].Role != "user" {
		t.Fatalf("messages = %+v, want system then user", req.Messages)
	}
	if req.MaxCompletionTokens == nil || *req.MaxCompletionTokens != 256 {
		t.Fatalf("max_completion_tokens = %v, want 256", req.MaxCompletionTokens)
	}
	if req.MaxTokens != nil {
		t.Fatalf("max_tokens = %v, want nil for modern field", req.MaxTokens)
	}
	if req.ReasoningEffort != "low" {
		t.Fatalf("reasoning_effort = %q, want low", req.ReasoningEffort)
	}
	if req.ServiceTier != ServiceTierDefault {
		t.Fatalf("service_tier = %q, want default", req.ServiceTier)
	}
	if req.StreamOptions == nil || !req.StreamOptions.IncludeUsage {
		t.Fatalf("stream_options.include_usage should be true, got %+v", req.StreamOptions)
	}
	if req.Seed == nil || *req.Seed != 7 {
		t.Fatalf("seed = %v, want 7", req.Seed)
	}
}

func assertObservation(t *testing.T, obs *testObserver) {
	t.Helper()

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

	if reasoning := obs.reasoningOutput(); len(reasoning) != 1 || reasoning[0].Text != "thinking hard" || reasoning[0].Visible {
		t.Fatalf("reasoning output = %+v, want one non-visible 'thinking hard'", reasoning)
	}

	usages := obs.usages()
	if len(usages) != 1 {
		t.Fatalf("usage count = %d, want 1", len(usages))
	}
	if usages[0].CompletionTokens == nil || *usages[0].CompletionTokens != 45 {
		t.Fatalf("completion tokens = %v, want 45", usages[0].CompletionTokens)
	}
	if usages[0].Extra["reasoning_tokens"] != 8 {
		t.Fatalf("usage extra reasoning_tokens = %v, want 8", usages[0].Extra["reasoning_tokens"])
	}

	caches := obs.caches()
	if len(caches) != 1 || caches[0].Hit == nil || !*caches[0].Hit {
		t.Fatalf("cache = %+v, want hit=true", caches)
	}
	if caches[0].PromptCachedTokens == nil || *caches[0].PromptCachedTokens != 32 {
		t.Fatalf("prompt cached tokens = %v, want 32", caches[0].PromptCachedTokens)
	}

	timing := obs.latestHTTP().ServerTiming
	if timing == nil {
		t.Fatal("server timing should be captured from time_info")
	}
	if timing.CompletionTimeMS == nil || *timing.CompletionTimeMS != 14 {
		t.Fatalf("completion_time_ms = %v, want 14 (0.014s)", timing.CompletionTimeMS)
	}
	if timing.TotalTimeMS == nil || *timing.TotalTimeMS != 90 {
		t.Fatalf("total_time_ms = %v, want 90 (0.09s)", timing.TotalTimeMS)
	}
	if obs.latestHTTP().ObservedServiceTier != "default" {
		t.Fatalf("observed service tier = %q, want default", obs.latestHTTP().ObservedServiceTier)
	}
}

// TestProviderStreamChatUsesServiceTierUsed verifies service_tier_used wins over service_tier when auto is requested.
func TestProviderStreamChatUsesServiceTierUsed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, w, `{"service_tier":"auto","service_tier_used":"flex","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`)
		writeSSE(t, w, "[DONE]")
	}))
	defer server.Close()

	provider := New(Config{BaseURL: server.URL, APIKey: "k", Model: "gpt-oss-120b", ServiceTier: ServiceTierAuto})
	obs := newTestObserver()
	if err := provider.StreamChat(context.Background(), whatttft.ProviderRequest{RequestID: "req-1", Prompt: "hi"}, obs); err != nil {
		t.Fatalf("stream chat: %v", err)
	}
	if got := obs.latestHTTP().ObservedServiceTier; got != "flex" {
		t.Fatalf("observed service tier = %q, want flex", got)
	}
}

// TestProviderStreamChatRedactsNon200Body verifies error bodies never leak the API key.
func TestProviderStreamChatRedactsNon200Body(t *testing.T) {
	const apiKey = "super-secret-key"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		if _, err := fmt.Fprintf(w, "rate limited for key %s", apiKey); err != nil {
			t.Errorf("write error body: %v", err)
		}
	}))
	defer server.Close()

	provider := New(Config{BaseURL: server.URL, APIKey: apiKey, Model: "gpt-oss-120b"})
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
	if obs.latestHTTP().StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status code = %d, want 429", obs.latestHTTP().StatusCode)
	}
}

// TestProviderStreamChatRejectsMalformedJSON verifies malformed chunks surface a decode error.
func TestProviderStreamChatRejectsMalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, w, `{"choices":[{"index":0,`)
	}))
	defer server.Close()

	provider := New(Config{BaseURL: server.URL, APIKey: "k", Model: "gpt-oss-120b"})
	obs := newTestObserver()
	err := provider.StreamChat(context.Background(), whatttft.ProviderRequest{RequestID: "req-1", Prompt: "hi"}, obs)
	if err == nil || !strings.Contains(err.Error(), "decode chat completion chunk") {
		t.Fatalf("error = %v, want decode chat completion chunk", err)
	}
}

// TestProviderCapabilitiesReflectConfig verifies stream-usage capability tracks IncludeUsage.
func TestProviderCapabilitiesReflectConfig(t *testing.T) {
	if New(Config{Model: "m"}).Capabilities().SupportsUsageInStream {
		t.Fatal("usage-in-stream should be false without IncludeUsage")
	}
	caps := New(Config{Model: "m", IncludeUsage: true}).Capabilities()
	if !caps.SupportsUsageInStream {
		t.Fatal("usage-in-stream should be true with IncludeUsage")
	}
	if caps.StreamingProtocol != streamProtocolSSE || !caps.SupportsChat || !caps.SupportsPromptCache {
		t.Fatalf("capabilities = %+v, unexpected", caps)
	}
	if New(Config{Model: "m"}).Name() != providerName {
		t.Fatal("name should be cerebras")
	}
}

// TestProviderStreamChatValidatesInputs verifies required-input validation before any request.
func TestProviderStreamChatValidatesInputs(t *testing.T) {
	if err := New(Config{APIKey: "k"}).StreamChat(context.Background(), whatttft.ProviderRequest{}, newTestObserver()); err == nil || !strings.Contains(err.Error(), "model is required") {
		t.Fatalf("missing model error = %v", err)
	}
	if err := New(Config{Model: "m"}).StreamChat(context.Background(), whatttft.ProviderRequest{}, newTestObserver()); err == nil || !strings.Contains(err.Error(), "API key is required") {
		t.Fatalf("missing key error = %v", err)
	}
	if err := New(Config{Model: "m", APIKey: "k", ServiceTier: "scale"}).StreamChat(context.Background(), whatttft.ProviderRequest{}, newTestObserver()); err == nil || !strings.Contains(err.Error(), "unsupported cerebras service tier") {
		t.Fatalf("bad service tier error = %v", err)
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
	cache    []whatttft.CacheRecord
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

func (o *testObserver) OnCache(cache whatttft.CacheRecord) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.cache = append(o.cache, cache)
}

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

func (o *testObserver) caches() []whatttft.CacheRecord {
	o.mu.Lock()
	defer o.mu.Unlock()

	return append([]whatttft.CacheRecord(nil), o.cache...)
}

func (o *testObserver) latestHTTP() whatttft.HTTPRecord {
	o.mu.Lock()
	defer o.mu.Unlock()

	if len(o.httpRecs) == 0 {
		return whatttft.HTTPRecord{}
	}

	return o.httpRecs[len(o.httpRecs)-1]
}
