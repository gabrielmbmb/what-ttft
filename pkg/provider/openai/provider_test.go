package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

// TestProviderStreamChatDefaultsToResponsesAPIParsesStreamingEvents verifies Responses streams are the default OpenAI path.
func TestProviderStreamChatDefaultsToResponsesAPIParsesStreamingEvents(t *testing.T) {
	const apiKey = "test-secret"

	requestCh := make(chan responseRequest, 1)
	handlerErrors := make(chan string, 8)
	var sawAuth atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			handlerErrors <- fmt.Sprintf("path = %q, want /responses", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			handlerErrors <- fmt.Sprintf("method = %q, want POST", r.Method)
			http.Error(w, "bad method", http.StatusMethodNotAllowed)
			return
		}
		if got := r.Header.Get("Authorization"); got == "Bearer "+apiKey {
			sawAuth.Store(true)
		} else {
			handlerErrors <- fmt.Sprintf("authorization header = %q", got)
		}
		if got := r.Header.Get("OpenAI-Organization"); got != "org-1" {
			handlerErrors <- fmt.Sprintf("organization header = %q, want org-1", got)
		}
		if got := r.Header.Get("OpenAI-Project"); got != "project-1" {
			handlerErrors <- fmt.Sprintf("project header = %q, want project-1", got)
		}

		var reqBody responseRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			handlerErrors <- fmt.Sprintf("decode request body: %v", err)
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		requestCh <- reqBody

		w.Header().Set(openAIProcessingMSHeader, "42")
		w.Header().Set("Content-Type", "text/event-stream")
		writeRaw(t, w, ": heartbeat\n\n")
		writeSSEEvent(t, w, "response.created", `{"type":"response.created","response":{"status":"in_progress","service_tier":"priority"}}`)
		writeSSEEvent(t, w, "response.content_part.added", `{"type":"response.content_part.added","part":{"type":"output_text","text":""}}`)
		writeSSEEvent(t, w, responseOutputTextDeltaEvent, `{"type":"response.output_text.delta","delta":"Hello"}`)
		writeSSEEvent(t, w, responseOutputTextDeltaEvent, `{"type":"response.output_text.delta","delta":" world"}`)
		writeSSEEvent(t, w, "response.completed", `{"type":"response.completed","response":{"status":"completed","service_tier":"priority","usage":{"input_tokens":10,"input_tokens_details":{"cached_tokens":0},"output_tokens":2,"output_tokens_details":{"reasoning_tokens":1},"total_tokens":12}}}`)
	}))
	defer server.Close()

	temperature := 0.0
	topP := 1.0
	provider := New(Config{
		BaseURL:      server.URL,
		APIKey:       apiKey,
		Organization: "org-1",
		Project:      "project-1",
		Model:        "gpt-test",
		ServiceTier:  ServiceTierPriority,
		IncludeUsage: true,
	})
	obs := newTestObserver()

	err := provider.StreamChat(context.Background(), whatttft.ProviderRequest{
		RequestID: "req-1",
		Scenario: whatttft.Scenario{
			SystemPrompt:    "You are concise.",
			MaxOutputTokens: 16,
			Temperature:     &temperature,
			TopP:            &topP,
			ReasoningEffort: "none",
		},
		Prompt: "Say hello.",
	}, obs)
	if err != nil {
		t.Fatalf("stream responses: %v", err)
	}
	assertNoHandlerErrors(t, handlerErrors)
	if !sawAuth.Load() {
		t.Fatal("authorization header was not observed")
	}

	requestBody := <-requestCh
	assertResponsesRequest(t, requestBody)
	assertSuccessfulResponsesObservation(t, obs)
}

// TestProviderStreamChatCompletionsCompatibilityParsesStreamingEvents verifies the explicit Chat Completions compatibility path.
func TestProviderStreamChatCompletionsCompatibilityParsesStreamingEvents(t *testing.T) {
	const apiKey = "test-secret"

	requestCh := make(chan chatCompletionRequest, 1)
	handlerErrors := make(chan string, 8)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			handlerErrors <- fmt.Sprintf("path = %q, want /chat/completions", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		var reqBody chatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			handlerErrors <- fmt.Sprintf("decode request body: %v", err)
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		requestCh <- reqBody

		w.Header().Set(openAIProcessingMSHeader, "42")
		w.Header().Set("Content-Type", "text/event-stream")
		writeRaw(t, w, ": heartbeat\n\n")
		writeSSE(t, w, "")
		writeSSE(t, w, `{"service_tier":"flex","choices":[{"index":0,"delta":{"role":"assistant"}}]}`)
		writeSSE(t, w, `{"service_tier":"flex","choices":[{"index":0,"delta":{"content":"Hello"}}]}`)
		writeSSE(t, w, `{"service_tier":"flex","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":"stop"}]}`)
		writeSSE(t, w, `{"service_tier":"flex","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12,"prompt_tokens_details":{"cached_tokens":0}}}`)
		writeSSE(t, w, "[DONE]")
	}))
	defer server.Close()

	temperature := 0.0
	topP := 1.0
	seed := int64(123)
	provider := New(Config{
		API:          ChatCompletionsAPI,
		BaseURL:      server.URL,
		APIKey:       apiKey,
		Model:        "gpt-test",
		ServiceTier:  ServiceTierFlex,
		IncludeUsage: true,
	})
	obs := newTestObserver()

	err := provider.StreamChat(context.Background(), whatttft.ProviderRequest{
		RequestID: "req-1",
		Scenario: whatttft.Scenario{
			SystemPrompt:    "You are concise.",
			MaxOutputTokens: 64,
			Temperature:     &temperature,
			TopP:            &topP,
			Stop:            []string{"END"},
			Seed:            &seed,
			ReasoningEffort: "none",
		},
		Prompt: "Say hello.",
	}, obs)
	if err != nil {
		t.Fatalf("stream chat completions: %v", err)
	}
	assertNoHandlerErrors(t, handlerErrors)

	requestBody := <-requestCh
	assertChatRequest(t, requestBody)
	assertSuccessfulChatObservation(t, obs)
}

// TestProviderStreamChatReturnsRedactedAPIError verifies non-2xx responses include bounded redacted details.
func TestProviderStreamChatReturnsRedactedAPIError(t *testing.T) {
	const apiKey = "test-secret"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "provider rejected token "+apiKey, http.StatusUnauthorized)
	}))
	defer server.Close()

	provider := New(Config{BaseURL: server.URL, APIKey: apiKey, Model: "gpt-test"})
	obs := newTestObserver()

	err := provider.StreamChat(context.Background(), whatttft.ProviderRequest{RequestID: "req-err", Prompt: "hello"}, obs)
	if err == nil {
		t.Fatal("expected API error")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status code = %d, want %d", apiErr.StatusCode, http.StatusUnauthorized)
	}
	if strings.Contains(apiErr.BodySnippet, apiKey) {
		t.Fatalf("body snippet leaked API key: %q", apiErr.BodySnippet)
	}
	if strings.Contains(err.Error(), apiKey) {
		t.Fatalf("error string leaked API key: %q", err.Error())
	}
	if obs.latestHTTP().StatusCode != http.StatusUnauthorized {
		t.Fatalf("observed HTTP status = %d, want %d", obs.latestHTTP().StatusCode, http.StatusUnauthorized)
	}
}

// TestProviderStreamChatMalformedJSON verifies malformed Responses events return decode errors without marking stream completion.
func TestProviderStreamChatMalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, w, "{not-json}")
	}))
	defer server.Close()

	provider := New(Config{BaseURL: server.URL, APIKey: "test-secret", Model: "gpt-test"})
	obs := newTestObserver()

	err := provider.StreamChat(context.Background(), whatttft.ProviderRequest{RequestID: "req-bad", Prompt: "hello"}, obs)
	if err == nil {
		t.Fatal("expected decode error")
	}
	if !strings.Contains(err.Error(), "decode response stream event") {
		t.Fatalf("error = %q, want decode context", err.Error())
	}
	if obs.eventCount(whatttft.EventDone) != 0 {
		t.Fatal("done_event should not be marked for malformed JSON")
	}
	if obs.eventCount(whatttft.EventBodyEOF) != 0 {
		t.Fatal("body_eof should not be marked for malformed JSON")
	}
}

// TestParseOpenAIProcessingMS verifies OpenAI processing headers are parsed conservatively.
func TestParseOpenAIProcessingMS(t *testing.T) {
	tests := map[string]struct {
		header string
		want   float64
		ok     bool
	}{
		"integer milliseconds": {header: "42", want: 42, ok: true},
		"decimal milliseconds": {header: "42.5", want: 42.5, ok: true},
		"missing":              {header: "", ok: false},
		"negative":             {header: "-1", ok: false},
		"nan":                  {header: "NaN", ok: false},
		"invalid":              {header: "slow", ok: false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			header := http.Header{}
			if tc.header != "" {
				header.Set(openAIProcessingMSHeader, tc.header)
			}

			got, ok := parseOpenAIProcessingMS(header)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if got != tc.want {
				t.Fatalf("processing ms = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestProviderChatRequestUsesLegacyMaxTokens verifies legacy max_tokens compatibility mode.
func TestProviderChatRequestUsesLegacyMaxTokens(t *testing.T) {
	provider := New(Config{API: ChatCompletionsAPI, Model: "gpt-test", UseLegacyMaxTokens: true})
	req := provider.chatRequest(whatttft.ProviderRequest{
		Scenario: whatttft.Scenario{MaxOutputTokens: 7},
		Prompt:   "hello",
	})

	if req.MaxTokens == nil || *req.MaxTokens != 7 {
		t.Fatalf("max_tokens = %v, want 7", req.MaxTokens)
	}
	if req.MaxCompletionTokens != nil {
		t.Fatalf("max_completion_tokens = %v, want nil", *req.MaxCompletionTokens)
	}
}

// TestProviderCapabilitiesReflectConfig verifies the provider advertises standardized capabilities.
func TestProviderCapabilitiesReflectConfig(t *testing.T) {
	responsesProvider := New(Config{Model: "gpt-test"})
	responsesCapabilities := responsesProvider.Capabilities()
	if !responsesCapabilities.SupportsUsageInStream {
		t.Fatal("Responses API should support usage in terminal stream events")
	}

	chatProvider := New(Config{API: ChatCompletionsAPI, Model: "gpt-test", IncludeUsage: true})
	capabilities := chatProvider.Capabilities()

	if chatProvider.Name() != providerName {
		t.Fatalf("provider name = %q, want %q", chatProvider.Name(), providerName)
	}
	if chatProvider.Model() != "gpt-test" {
		t.Fatalf("model = %q, want gpt-test", chatProvider.Model())
	}
	if capabilities.StreamingProtocol != streamProtocolSSE {
		t.Fatalf("stream protocol = %q, want %q", capabilities.StreamingProtocol, streamProtocolSSE)
	}
	if !capabilities.SupportsChat {
		t.Fatal("supports chat should be true")
	}
	if !capabilities.SupportsUsageInStream {
		t.Fatal("supports usage should reflect IncludeUsage")
	}
	if capabilities.SupportsTokenEvents {
		t.Fatal("OpenAI stream chunks are not true token events")
	}
}

// TestProviderStreamChatValidatesRequiredInputs verifies missing dependencies return clear errors.
func TestProviderStreamChatValidatesRequiredInputs(t *testing.T) {
	obs := newTestObserver()

	if err := New(Config{APIKey: "key"}).StreamChat(context.Background(), whatttft.ProviderRequest{}, obs); err == nil {
		t.Fatal("missing model should fail")
	}
	if err := New(Config{Model: "gpt-test"}).StreamChat(context.Background(), whatttft.ProviderRequest{}, obs); err == nil {
		t.Fatal("missing API key should fail")
	}
	if err := New(Config{Model: "gpt-test", APIKey: "key"}).StreamChat(context.Background(), whatttft.ProviderRequest{}, nil); err == nil {
		t.Fatal("missing observer should fail")
	}
	if err := New(Config{Model: "gpt-test", APIKey: "key", API: API("other")}).StreamChat(context.Background(), whatttft.ProviderRequest{}, obs); err == nil {
		t.Fatal("unsupported API should fail")
	}
	if err := New(Config{Model: "gpt-test", APIKey: "key", ServiceTier: ServiceTier("vip")}).StreamChat(context.Background(), whatttft.ProviderRequest{}, obs); err == nil {
		t.Fatal("unsupported service tier should fail")
	}
	if err := New(Config{Model: "gpt-test", APIKey: "key", UseLegacyMaxTokens: true}).StreamChat(context.Background(), whatttft.ProviderRequest{}, obs); err == nil {
		t.Fatal("legacy max_tokens with Responses should fail")
	}
	if err := New(Config{Model: "gpt-test", APIKey: "key"}).StreamChat(context.Background(), whatttft.ProviderRequest{Scenario: whatttft.Scenario{MaxOutputTokens: 8}}, obs); err == nil {
		t.Fatal("Responses max_output_tokens below minimum should fail")
	}
}

func assertResponsesRequest(t *testing.T, req responseRequest) {
	t.Helper()

	if req.Model != "gpt-test" {
		t.Fatalf("model = %q, want gpt-test", req.Model)
	}
	if req.Input != "Say hello." {
		t.Fatalf("input = %q, want prompt", req.Input)
	}
	if req.Instructions != "You are concise." {
		t.Fatalf("instructions = %q, want system prompt", req.Instructions)
	}
	if !req.Stream {
		t.Fatal("stream should be true")
	}
	if req.StreamOptions == nil || req.StreamOptions.IncludeObfuscation == nil || *req.StreamOptions.IncludeObfuscation {
		t.Fatalf("stream options = %#v, want include_obfuscation false", req.StreamOptions)
	}
	if req.MaxOutputTokens == nil || *req.MaxOutputTokens != 16 {
		t.Fatalf("max_output_tokens = %v, want 16", req.MaxOutputTokens)
	}
	if req.Reasoning == nil || req.Reasoning.Effort != "none" {
		t.Fatalf("reasoning = %#v, want none", req.Reasoning)
	}
	if req.ServiceTier != ServiceTierPriority {
		t.Fatalf("service_tier = %q, want priority", req.ServiceTier)
	}
}

func assertChatRequest(t *testing.T, req chatCompletionRequest) {
	t.Helper()

	if req.Model != "gpt-test" {
		t.Fatalf("model = %q, want gpt-test", req.Model)
	}
	if !req.Stream {
		t.Fatal("stream should be true")
	}
	if req.StreamOptions == nil || !req.StreamOptions.IncludeUsage {
		t.Fatalf("stream options = %#v, want include_usage", req.StreamOptions)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(req.Messages))
	}
	if req.Messages[0].Role != "system" || req.Messages[0].Content != "You are concise." {
		t.Fatalf("system message = %#v", req.Messages[0])
	}
	if req.Messages[1].Role != "user" || req.Messages[1].Content != "Say hello." {
		t.Fatalf("user message = %#v", req.Messages[1])
	}
	if req.MaxCompletionTokens == nil || *req.MaxCompletionTokens != 64 {
		t.Fatalf("max_completion_tokens = %v, want 64", req.MaxCompletionTokens)
	}
	if req.ReasoningEffort != "none" {
		t.Fatalf("reasoning_effort = %q, want none", req.ReasoningEffort)
	}
	if req.ServiceTier != ServiceTierFlex {
		t.Fatalf("service_tier = %q, want flex", req.ServiceTier)
	}
	if req.MaxTokens != nil {
		t.Fatalf("max_tokens = %v, want nil", *req.MaxTokens)
	}
}

func assertSuccessfulResponsesObservation(t *testing.T, obs *testObserver) {
	t.Helper()

	for _, name := range []whatttft.EventName{
		whatttft.EventRequestStart,
		whatttft.EventHeadersReceived,
		whatttft.EventFirstSSEEvent,
		whatttft.EventFirstOutputDelta,
		whatttft.EventLastOutputDelta,
		whatttft.EventDone,
		whatttft.EventBodyEOF,
	} {
		if obs.eventCount(name) == 0 {
			t.Fatalf("event %q was not marked", name)
		}
	}

	visible := obs.visibleOutput()
	if len(visible) != 2 {
		t.Fatalf("visible output count = %d, want 2", len(visible))
	}
	if visible[0].Text != "Hello" || visible[1].Text != " world" {
		t.Fatalf("visible output = %#v, want Hello/world", visible)
	}

	usageRecords := obs.usages()
	if len(usageRecords) != 1 {
		t.Fatalf("usage count = %d, want 1", len(usageRecords))
	}
	if usageRecords[0].PromptTokens == nil || *usageRecords[0].PromptTokens != 10 {
		t.Fatalf("prompt tokens = %v, want 10", usageRecords[0].PromptTokens)
	}
	if usageRecords[0].CompletionTokens == nil || *usageRecords[0].CompletionTokens != 2 {
		t.Fatalf("completion tokens = %v, want 2", usageRecords[0].CompletionTokens)
	}
	if usageRecords[0].Extra["reasoning_tokens"] != 1 {
		t.Fatalf("usage extra = %#v, want reasoning_tokens 1", usageRecords[0].Extra)
	}

	cacheRecords := obs.caches()
	if len(cacheRecords) != 1 {
		t.Fatalf("cache count = %d, want 1", len(cacheRecords))
	}
	if cacheRecords[0].Hit == nil || *cacheRecords[0].Hit {
		t.Fatalf("cache hit = %v, want pointer to false", cacheRecords[0].Hit)
	}

	finishEvents := obs.finishes()
	if len(finishEvents) != 1 || !finishEvents[0].Terminal {
		t.Fatalf("finish events = %#v, want one terminal event", finishEvents)
	}
	httpRecord := obs.latestHTTP()
	if httpRecord.StatusCode != http.StatusOK {
		t.Fatalf("observed HTTP status = %d, want 200", httpRecord.StatusCode)
	}
	if httpRecord.RequestedServiceTier != string(ServiceTierPriority) || httpRecord.ObservedServiceTier != string(ServiceTierPriority) {
		t.Fatalf("HTTP service tiers = requested %q observed %q, want priority/priority", httpRecord.RequestedServiceTier, httpRecord.ObservedServiceTier)
	}
	if httpRecord.ProviderProcessingMS == nil || *httpRecord.ProviderProcessingMS != 42 {
		t.Fatalf("provider processing ms = %v, want 42", httpRecord.ProviderProcessingMS)
	}
}

func assertSuccessfulChatObservation(t *testing.T, obs *testObserver) {
	t.Helper()

	for _, name := range []whatttft.EventName{
		whatttft.EventRequestStart,
		whatttft.EventHeadersReceived,
		whatttft.EventFirstSSEEvent,
		whatttft.EventFirstOutputDelta,
		whatttft.EventLastOutputDelta,
		whatttft.EventDone,
		whatttft.EventBodyEOF,
	} {
		if obs.eventCount(name) == 0 {
			t.Fatalf("event %q was not marked", name)
		}
	}
	if got := obs.eventCount(whatttft.EventFirstOutputDelta); got != 1 {
		t.Fatalf("first_output_delta marks = %d, want 1", got)
	}

	streamEvents := obs.streamEvents()
	if len(streamEvents) != 6 {
		t.Fatalf("stream event count = %d, want 6", len(streamEvents))
	}
	if !streamEvents[0].Empty {
		t.Fatal("first returned SSE event should be explicit empty data")
	}
	if !streamEvents[len(streamEvents)-1].Terminal {
		t.Fatal("last stream event should be terminal [DONE]")
	}

	visible := obs.visibleOutput()
	if len(visible) != 2 {
		t.Fatalf("visible output count = %d, want 2", len(visible))
	}
	if visible[0].Text != "Hello" || visible[1].Text != " world" {
		t.Fatalf("visible output = %#v, want Hello/world", visible)
	}
	if visible[1].FinishReason != "stop" {
		t.Fatalf("finish reason = %q, want stop", visible[1].FinishReason)
	}

	usageRecords := obs.usages()
	if len(usageRecords) != 1 {
		t.Fatalf("usage count = %d, want 1", len(usageRecords))
	}
	if usageRecords[0].PromptTokens == nil || *usageRecords[0].PromptTokens != 10 {
		t.Fatalf("prompt tokens = %v, want 10", usageRecords[0].PromptTokens)
	}
	if usageRecords[0].CompletionTokens == nil || *usageRecords[0].CompletionTokens != 2 {
		t.Fatalf("completion tokens = %v, want 2", usageRecords[0].CompletionTokens)
	}

	cacheRecords := obs.caches()
	if len(cacheRecords) != 1 {
		t.Fatalf("cache count = %d, want 1", len(cacheRecords))
	}
	if cacheRecords[0].Hit == nil || *cacheRecords[0].Hit {
		t.Fatalf("cache hit = %v, want pointer to false", cacheRecords[0].Hit)
	}
	if cacheRecords[0].PromptCachedTokens == nil || *cacheRecords[0].PromptCachedTokens != 0 {
		t.Fatalf("cached tokens = %v, want 0", cacheRecords[0].PromptCachedTokens)
	}

	finishEvents := obs.finishes()
	if len(finishEvents) != 2 {
		t.Fatalf("finish event count = %d, want stop and terminal", len(finishEvents))
	}
	if !finishEvents[1].Terminal {
		t.Fatalf("second finish event = %#v, want terminal", finishEvents[1])
	}
	httpRecord := obs.latestHTTP()
	if httpRecord.StatusCode != http.StatusOK {
		t.Fatalf("observed HTTP status = %d, want 200", httpRecord.StatusCode)
	}
	if httpRecord.RequestedServiceTier != string(ServiceTierFlex) || httpRecord.ObservedServiceTier != string(ServiceTierFlex) {
		t.Fatalf("HTTP service tiers = requested %q observed %q, want flex/flex", httpRecord.RequestedServiceTier, httpRecord.ObservedServiceTier)
	}
	if httpRecord.ProviderProcessingMS == nil || *httpRecord.ProviderProcessingMS != 42 {
		t.Fatalf("provider processing ms = %v, want 42", httpRecord.ProviderProcessingMS)
	}
}

func assertNoHandlerErrors(t *testing.T, errors <-chan string) {
	t.Helper()

	for {
		select {
		case err := <-errors:
			t.Fatal(err)
		default:
			return
		}
	}
}

func writeSSEEvent(t *testing.T, w http.ResponseWriter, event string, data string) {
	t.Helper()
	writeRaw(t, w, "event: "+event+"\n")
	writeSSE(t, w, data)
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

type testObserver struct {
	mu       sync.Mutex
	events   map[whatttft.EventName]int
	streams  []whatttft.StreamEvent
	outputs  []whatttft.OutputDelta
	tokens   []whatttft.TokenEvent
	usage    []whatttft.ProviderUsage
	cache    []whatttft.CacheRecord
	finish   []whatttft.FinishEvent
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

func (o *testObserver) OnStreamEvent(event whatttft.StreamEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.streams = append(o.streams, event)
}

func (o *testObserver) OnOutputDelta(delta whatttft.OutputDelta) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.outputs = append(o.outputs, delta)
}

func (o *testObserver) OnToken(event whatttft.TokenEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.tokens = append(o.tokens, event)
}

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

func (o *testObserver) OnFinish(event whatttft.FinishEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.finish = append(o.finish, event)
}

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

func (o *testObserver) streamEvents() []whatttft.StreamEvent {
	o.mu.Lock()
	defer o.mu.Unlock()

	return append([]whatttft.StreamEvent(nil), o.streams...)
}

func (o *testObserver) visibleOutput() []whatttft.OutputDelta {
	o.mu.Lock()
	defer o.mu.Unlock()

	visible := make([]whatttft.OutputDelta, 0, len(o.outputs))
	for _, output := range o.outputs {
		if output.Visible {
			visible = append(visible, output)
		}
	}

	return visible
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

func (o *testObserver) finishes() []whatttft.FinishEvent {
	o.mu.Lock()
	defer o.mu.Unlock()

	return append([]whatttft.FinishEvent(nil), o.finish...)
}

func (o *testObserver) latestHTTP() whatttft.HTTPRecord {
	o.mu.Lock()
	defer o.mu.Unlock()

	if len(o.httpRecs) == 0 {
		return whatttft.HTTPRecord{}
	}

	return o.httpRecs[len(o.httpRecs)-1]
}
