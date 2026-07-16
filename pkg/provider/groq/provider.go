package groq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gabrielmbmb/what-ttft/internal/httptracecap"
	"github.com/gabrielmbmb/what-ttft/internal/sse"
	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

const (
	// maxErrorBodyBytes bounds non-2xx response bodies included in APIError diagnostics.
	maxErrorBodyBytes = 64 * 1024

	// providerName is the normalized provider identifier emitted in request records.
	providerName = "groq"

	// streamProtocolSSE identifies data-only Server-Sent Events streams.
	streamProtocolSSE = "sse"

	// textModality identifies user-visible text output deltas.
	textModality = "text"

	// reasoningModality identifies hidden reasoning output deltas that must never drive TTFT.
	reasoningModality = "reasoning"

	// secondsToMS converts provider-reported seconds into the milliseconds used across the benchmark schema.
	secondsToMS = 1000.0

	// responseOutputTextDeltaEvent is the Responses stream event carrying visible output text.
	responseOutputTextDeltaEvent = "response.output_text.delta"

	// responseRefusalDeltaEvent is the Responses stream event carrying visible refusal text.
	responseRefusalDeltaEvent = "response.refusal.delta"
)

// Provider streams Groq requests over direct HTTP for the Chat Completions and Responses APIs.
type Provider struct {
	cfg                 Config
	client              *http.Client
	compressionDisabled bool
}

// APIError is a structured, redacted non-2xx provider response error.
type APIError struct {
	// StatusCode is the HTTP status code returned by the provider; zero means no response status was available.
	StatusCode int

	// Status is the HTTP status text returned by the provider; empty means no response status was available.
	Status string

	// BodySnippet is a bounded, redacted response-body excerpt; empty means no body was available.
	BodySnippet string
}

// Error returns a redacted human-readable provider error string.
func (e *APIError) Error() string {
	if e == nil {
		return "groq API error"
	}
	if e.BodySnippet == "" {
		return fmt.Sprintf("groq request failed: %s", e.Status)
	}

	return fmt.Sprintf("groq request failed: %s: %s", e.Status, e.BodySnippet)
}

// New creates a Groq streaming provider.
func New(cfg Config) *Provider {
	client := cfg.HTTPClient
	compressionDisabled := transportDisablesCompression(client)
	if client == nil {
		client = httptracecap.NewHTTPClient(httptracecap.TransportConfig{})
		compressionDisabled = true
	}

	return &Provider{
		cfg:                 cfg,
		client:              client,
		compressionDisabled: compressionDisabled,
	}
}

// Name returns the normalized Groq provider name.
func (p *Provider) Name() string {
	return providerName
}

// Model returns the configured model identifier.
func (p *Provider) Model() string {
	return p.cfg.Model
}

// Capabilities returns Groq streaming capabilities exposed by this adapter.
func (p *Provider) Capabilities() whatttft.ProviderCapabilities {
	supportsUsage := true
	if p.cfg.api() == ChatCompletionsAPI {
		supportsUsage = p.cfg.IncludeUsage
	}

	return whatttft.ProviderCapabilities{
		StreamingProtocol:     streamProtocolSSE,
		SupportsChat:          true,
		SupportsUsageInStream: supportsUsage,
		SupportsPromptCache:   false,
		SupportsExplicitCache: false,
		SupportsTokenEvents:   false,
	}
}

// StreamChat sends one streaming request and reports standardized benchmark events to obs.
func (p *Provider) StreamChat(ctx context.Context, req whatttft.ProviderRequest, obs whatttft.ProviderObserver) error {
	apiKey, err := p.validateInputs(obs)
	if err != nil {
		return err
	}

	switch p.cfg.api() {
	case ResponsesAPI:
		return p.streamResponses(ctx, req, obs, apiKey)
	case ChatCompletionsAPI:
		return p.streamChatCompletions(ctx, req, obs, apiKey)
	default:
		return fmt.Errorf("unsupported groq API %q", p.cfg.api())
	}
}

func (p *Provider) validateInputs(obs whatttft.ProviderObserver) (string, error) {
	if obs == nil {
		return "", errors.New("groq provider observer is nil")
	}
	if p.cfg.Model == "" {
		return "", errors.New("groq model is required")
	}
	if !ValidAPI(p.cfg.API) {
		return "", fmt.Errorf("unsupported groq API %q", p.cfg.API)
	}
	if !ValidServiceTier(p.cfg.ServiceTier) {
		return "", fmt.Errorf("unsupported groq service tier %q", p.cfg.ServiceTier)
	}

	apiKey := p.cfg.apiKey()
	if apiKey == "" {
		return "", errors.New("groq API key is required")
	}

	return apiKey, nil
}

func (p *Provider) streamChatCompletions(ctx context.Context, req whatttft.ProviderRequest, obs whatttft.ProviderObserver, apiKey string) error {
	body, err := json.Marshal(p.chatRequest(req))
	if err != nil {
		return fmt.Errorf("encode chat completion request: %w", err)
	}

	resp, capture, err := p.doStreamingRequest(ctx, p.cfg.chatCompletionsEndpointURL(), body, apiKey, obs)
	if err != nil {
		return err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		apiErr := p.readAPIError(resp, apiKey)
		p.observeHTTP(obs, capture, "", nil)
		return apiErr
	}

	observedServiceTier, timing, err := p.readChatCompletionsStream(resp, req.RequestID, obs)
	p.observeHTTP(obs, capture, observedServiceTier, timing)
	if err != nil {
		return err
	}

	return nil
}

func (p *Provider) streamResponses(ctx context.Context, req whatttft.ProviderRequest, obs whatttft.ProviderObserver, apiKey string) error {
	responseReq := p.responseRequest(req)
	body, err := json.Marshal(responseReq)
	if err != nil {
		return fmt.Errorf("encode responses request: %w", err)
	}

	resp, capture, err := p.doStreamingRequest(ctx, p.cfg.responsesEndpointURL(), body, apiKey, obs)
	if err != nil {
		return err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		apiErr := p.readAPIError(resp, apiKey)
		p.observeHTTP(obs, capture, "", nil)
		return apiErr
	}

	observedServiceTier, err := p.readResponsesStream(resp, req.RequestID, obs)
	p.observeHTTP(obs, capture, observedServiceTier, nil)
	if err != nil {
		return err
	}

	return nil
}

func (p *Provider) doStreamingRequest(ctx context.Context, endpoint string, body []byte, apiKey string, obs whatttft.ProviderObserver) (*http.Response, *httptracecap.Capture, error) {
	capture := httptracecap.NewCapture(httptracecap.CaptureConfig{CompressionDisabled: p.compressionDisabled})
	tracedCtx := httptracecap.WithTrace(ctx, obs, capture)
	httpReq, err := http.NewRequestWithContext(tracedCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, capture, fmt.Errorf("create groq request: %w", err)
	}
	p.setHeaders(httpReq, apiKey)

	obs.Mark(whatttft.EventRequestStart)
	resp, err := p.client.Do(httpReq)
	if err != nil {
		p.observeHTTP(obs, capture, "", nil)
		return nil, capture, fmt.Errorf("send groq request: %w", err)
	}

	obs.Mark(whatttft.EventHeadersReceived)
	capture.ObserveResponse(resp)
	p.observeHTTP(obs, capture, "", nil)

	return resp, capture, nil
}

func (p *Provider) chatRequest(req whatttft.ProviderRequest) chatCompletionRequest {
	messages := make([]chatMessage, 0, 2)
	if req.Scenario.SystemPrompt != "" {
		messages = append(messages, chatMessage{Role: "system", Content: req.Scenario.SystemPrompt})
	}
	messages = append(messages, chatMessage{Role: "user", Content: req.Prompt})

	chatReq := chatCompletionRequest{
		Model:            p.cfg.Model,
		Messages:         messages,
		Stream:           true,
		Temperature:      req.Scenario.Temperature,
		TopP:             req.Scenario.TopP,
		FrequencyPenalty: req.Scenario.FrequencyPenalty,
		PresencePenalty:  req.Scenario.PresencePenalty,
		Stop:             req.Scenario.Stop,
		Seed:             req.Scenario.Seed,
		ReasoningEffort:  req.Scenario.ReasoningEffort,
		ServiceTier:      p.cfg.ServiceTier,
	}

	if p.cfg.IncludeUsage {
		chatReq.StreamOptions = &chatStreamOptions{IncludeUsage: true}
	}
	if req.Scenario.MaxOutputTokens > 0 {
		maxTokens := req.Scenario.MaxOutputTokens
		if p.cfg.UseLegacyMaxTokens {
			chatReq.MaxTokens = &maxTokens
		} else {
			chatReq.MaxCompletionTokens = &maxTokens
		}
	}

	return chatReq
}

func (p *Provider) responseRequest(req whatttft.ProviderRequest) responseRequest {
	responseReq := responseRequest{
		Model:       p.cfg.Model,
		Input:       req.Prompt,
		Stream:      true,
		Temperature: req.Scenario.Temperature,
		TopP:        req.Scenario.TopP,
		ServiceTier: p.cfg.ServiceTier,
	}
	if req.Scenario.SystemPrompt != "" {
		responseReq.Instructions = req.Scenario.SystemPrompt
	}
	if req.Scenario.ReasoningEffort != "" {
		responseReq.Reasoning = &responseReasoning{Effort: req.Scenario.ReasoningEffort}
	}
	if req.Scenario.MaxOutputTokens > 0 {
		maxTokens := req.Scenario.MaxOutputTokens
		responseReq.MaxOutputTokens = &maxTokens
	}

	return responseReq
}

func (p *Provider) setHeaders(req *http.Request, apiKey string) {
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
}

func (p *Provider) observeHTTP(obs whatttft.ProviderObserver, capture *httptracecap.Capture, observedServiceTier ServiceTier, timing *whatttft.ServerTiming) {
	record := capture.Record()
	record.RequestedServiceTier = string(p.cfg.ServiceTier)
	record.ObservedServiceTier = string(observedServiceTier)
	record.ServerTiming = timing
	obs.OnHTTP(record)
}

func (p *Provider) readAPIError(resp *http.Response, apiKey string) *APIError {
	defer closeResponseBody(resp.Body)

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	if err != nil {
		body = []byte("read error body: " + err.Error())
	}

	return &APIError{
		StatusCode:  resp.StatusCode,
		Status:      resp.Status,
		BodySnippet: redactSecret(string(body), apiKey),
	}
}

func (p *Provider) readChatCompletionsStream(resp *http.Response, requestID string, obs whatttft.ProviderObserver) (ServiceTier, *whatttft.ServerTiming, error) {
	parser := sse.New(resp.Body)
	streamIndex := 0
	outputIndex := 0
	var observedServiceTier ServiceTier
	var timing *whatttft.ServerTiming

	for {
		event, err := parser.Next()
		if errors.Is(err, io.EOF) {
			if closeErr := resp.Body.Close(); closeErr != nil {
				return observedServiceTier, timing, fmt.Errorf("close chat completion stream: %w", closeErr)
			}
			obs.Mark(whatttft.EventBodyEOF)
			return observedServiceTier, timing, nil
		}
		if err != nil {
			closeResponseBody(resp.Body)
			return observedServiceTier, timing, fmt.Errorf("read SSE event: %w", err)
		}

		data := bytes.TrimSpace(event.Data)
		terminal := bytes.Equal(data, []byte("[DONE]"))
		obs.MarkFirst(whatttft.EventFirstSSEEvent)
		obs.OnStreamEvent(whatttft.StreamEvent{
			RequestID: requestID,
			Index:     streamIndex,
			Protocol:  streamProtocolSSE,
			RawBytes:  event.RawBytes,
			DataBytes: len(event.Data),
			Empty:     len(data) == 0,
			Terminal:  terminal,
		})
		streamIndex++

		if terminal {
			obs.Mark(whatttft.EventDone)
			obs.OnFinish(whatttft.FinishEvent{RequestID: requestID, Terminal: true})
			continue
		}
		if len(data) == 0 {
			continue
		}

		var chunk chatCompletionChunk
		if err := json.Unmarshal(data, &chunk); err != nil {
			closeResponseBody(resp.Body)
			return observedServiceTier, timing, fmt.Errorf("decode chat completion chunk: %w", err)
		}
		if chunk.ServiceTier != "" {
			observedServiceTier = chunk.ServiceTier
		}

		// Groq reports terminal usage and timing either directly in usage or nested in x_groq.usage.
		if effectiveUsage := chunkUsage(chunk); effectiveUsage != nil {
			obs.OnUsage(normalizeUsage(effectiveUsage))
			if t := normalizeTiming(effectiveUsage); t != nil {
				timing = t
			}
		}

		for _, ch := range chunk.Choices {
			outputIndex = p.observeChoice(obs, requestID, ch, outputIndex)
		}
	}
}

func (p *Provider) observeChoice(obs whatttft.ProviderObserver, requestID string, ch choice, outputIndex int) int {
	// Reasoning is hidden output: record it for diagnostics but never let it drive TTFT.
	if ch.Delta.Reasoning != "" {
		obs.OnOutputDelta(whatttft.OutputDelta{
			RequestID: requestID,
			Index:     outputIndex,
			Text:      ch.Delta.Reasoning,
			Role:      ch.Delta.Role,
			Modality:  reasoningModality,
			Visible:   false,
		})
		outputIndex++
	}

	if ch.Delta.Content == "" {
		if ch.Delta.Role != "" || ch.FinishReason != "" {
			obs.OnOutputDelta(whatttft.OutputDelta{
				RequestID:    requestID,
				Index:        outputIndex,
				Role:         ch.Delta.Role,
				Modality:     textModality,
				Visible:      false,
				FinishReason: ch.FinishReason,
			})
			outputIndex++
		}
		if ch.FinishReason != "" {
			obs.OnFinish(whatttft.FinishEvent{RequestID: requestID, FinishReason: ch.FinishReason})
		}

		return outputIndex
	}

	obs.MarkFirst(whatttft.EventFirstOutputDelta)
	obs.MarkLast(whatttft.EventLastOutputDelta)
	obs.OnOutputDelta(whatttft.OutputDelta{
		RequestID:    requestID,
		Index:        outputIndex,
		Text:         ch.Delta.Content,
		Role:         ch.Delta.Role,
		Modality:     textModality,
		Visible:      true,
		FinishReason: ch.FinishReason,
	})
	outputIndex++

	if ch.FinishReason != "" {
		obs.OnFinish(whatttft.FinishEvent{RequestID: requestID, FinishReason: ch.FinishReason})
	}

	return outputIndex
}

func (p *Provider) readResponsesStream(resp *http.Response, requestID string, obs whatttft.ProviderObserver) (ServiceTier, error) {
	parser := sse.New(resp.Body)
	streamIndex := 0
	outputIndex := 0
	var observedServiceTier ServiceTier

	for {
		event, err := parser.Next()
		if errors.Is(err, io.EOF) {
			if closeErr := resp.Body.Close(); closeErr != nil {
				return observedServiceTier, fmt.Errorf("close responses stream: %w", closeErr)
			}
			obs.Mark(whatttft.EventBodyEOF)
			return observedServiceTier, nil
		}
		if err != nil {
			closeResponseBody(resp.Body)
			return observedServiceTier, fmt.Errorf("read Responses SSE event: %w", err)
		}

		data := bytes.TrimSpace(event.Data)
		terminal := bytes.Equal(data, []byte("[DONE]"))
		obs.MarkFirst(whatttft.EventFirstSSEEvent)
		obs.OnStreamEvent(whatttft.StreamEvent{
			RequestID: requestID,
			Index:     streamIndex,
			Protocol:  streamProtocolSSE,
			RawBytes:  event.RawBytes,
			DataBytes: len(event.Data),
			Empty:     len(data) == 0,
			Terminal:  terminal,
		})
		streamIndex++

		if terminal {
			obs.Mark(whatttft.EventDone)
			obs.OnFinish(whatttft.FinishEvent{RequestID: requestID, Terminal: true})
			continue
		}
		if len(data) == 0 {
			continue
		}

		var responseEvent responseStreamEvent
		if err := json.Unmarshal(data, &responseEvent); err != nil {
			closeResponseBody(resp.Body)
			return observedServiceTier, fmt.Errorf("decode response stream event: %w", err)
		}
		if responseEvent.Type == "" {
			closeResponseBody(resp.Body)
			return observedServiceTier, errors.New("decode response stream event: missing type")
		}
		if tier := serviceTierFromResponse(responseEvent.Response); tier != "" {
			observedServiceTier = tier
		}

		if err := p.handleResponseEvent(responseEvent, requestID, obs, &outputIndex); err != nil {
			closeResponseBody(resp.Body)
			return observedServiceTier, err
		}
	}
}

func (p *Provider) handleResponseEvent(responseEvent responseStreamEvent, requestID string, obs whatttft.ProviderObserver, outputIndex *int) error {
	switch responseEvent.Type {
	case responseOutputTextDeltaEvent, responseRefusalDeltaEvent:
		if responseEvent.Delta == "" {
			return nil
		}
		obs.MarkFirst(whatttft.EventFirstOutputDelta)
		obs.MarkLast(whatttft.EventLastOutputDelta)
		obs.OnOutputDelta(whatttft.OutputDelta{
			RequestID: requestID,
			Index:     *outputIndex,
			Text:      responseEvent.Delta,
			Modality:  textModality,
			Visible:   true,
		})
		*outputIndex++
	case "response.completed":
		if responseEvent.Response != nil {
			observeResponsesUsage(obs, responseEvent.Response.Usage)
		}
		obs.Mark(whatttft.EventDone)
		obs.OnFinish(whatttft.FinishEvent{RequestID: requestID, FinishReason: "completed", Terminal: true})
	case "response.incomplete":
		finishReason := "incomplete"
		if responseEvent.Response != nil {
			observeResponsesUsage(obs, responseEvent.Response.Usage)
			if responseEvent.Response.IncompleteDetails != nil && responseEvent.Response.IncompleteDetails.Reason != "" {
				finishReason = responseEvent.Response.IncompleteDetails.Reason
			}
		}
		obs.Mark(whatttft.EventDone)
		obs.OnFinish(whatttft.FinishEvent{RequestID: requestID, FinishReason: finishReason, Terminal: true})
	case "response.failed":
		obs.Mark(whatttft.EventDone)
		obs.OnFinish(whatttft.FinishEvent{RequestID: requestID, FinishReason: "failed", Terminal: true})
		return responseFailureError(responseEvent.Response)
	case "error":
		return responseEventError(responseEvent)
	}

	return nil
}

// chunkUsage returns the effective usage payload for a chunk, preferring the top-level usage and
// falling back to Groq's x_groq.usage carried on the terminal streaming chunk.
func chunkUsage(chunk chatCompletionChunk) *usage {
	if chunk.Usage != nil {
		return chunk.Usage
	}
	if chunk.XGroq != nil {
		return chunk.XGroq.Usage
	}

	return nil
}

func normalizeUsage(raw *usage) whatttft.ProviderUsage {
	return whatttft.ProviderUsage{
		PromptTokens:     intPointer(raw.PromptTokens),
		CompletionTokens: intPointer(raw.CompletionTokens),
		TotalTokens:      intPointer(raw.TotalTokens),
		Source:           "provider-reported",
	}
}

// normalizeTiming converts Groq's server-reported seconds into a ServerTiming in milliseconds, or
// returns nil when Groq reported no timing fields on this usage payload.
func normalizeTiming(raw *usage) *whatttft.ServerTiming {
	if raw.QueueTime == 0 && raw.PromptTime == 0 && raw.CompletionTime == 0 && raw.TotalTime == 0 {
		return nil
	}

	return &whatttft.ServerTiming{
		QueueTimeMS:      msPointer(raw.QueueTime),
		PromptTimeMS:     msPointer(raw.PromptTime),
		CompletionTimeMS: msPointer(raw.CompletionTime),
		TotalTimeMS:      msPointer(raw.TotalTime),
	}
}

func observeResponsesUsage(obs whatttft.ProviderObserver, raw *responseUsage) {
	if raw == nil {
		return
	}

	providerUsage := whatttft.ProviderUsage{
		PromptTokens:     intPointer(raw.InputTokens),
		CompletionTokens: intPointer(raw.OutputTokens),
		TotalTokens:      intPointer(raw.TotalTokens),
		Source:           "provider-reported",
	}
	if raw.OutputTokensDetails != nil {
		providerUsage.Extra = map[string]any{"reasoning_tokens": raw.OutputTokensDetails.ReasoningTokens}
	}
	obs.OnUsage(providerUsage)
}

func serviceTierFromResponse(response *responseObject) ServiceTier {
	if response == nil {
		return ""
	}

	return response.ServiceTier
}

func responseFailureError(response *responseObject) error {
	if response == nil || response.Error == nil {
		return errors.New("responses stream failed")
	}
	if response.Error.Code == "" {
		return fmt.Errorf("responses stream failed: %s", response.Error.Message)
	}

	return fmt.Errorf("responses stream failed: %s: %s", response.Error.Code, response.Error.Message)
}

func responseEventError(event responseStreamEvent) error {
	code := ""
	if event.Code != nil {
		code = *event.Code
	}
	if code == "" {
		return fmt.Errorf("responses stream error: %s", event.Message)
	}

	return fmt.Errorf("responses stream error: %s: %s", code, event.Message)
}

func msPointer(seconds float64) *float64 {
	ms := seconds * secondsToMS
	return &ms
}

func intPointer(value int) *int {
	return &value
}

func redactSecret(value string, secret string) string {
	if secret == "" {
		return value
	}

	return strings.ReplaceAll(value, secret, "[REDACTED]")
}

func transportDisablesCompression(client *http.Client) bool {
	if client == nil || client.Transport == nil {
		return false
	}

	transport, ok := client.Transport.(*http.Transport)
	return ok && transport.DisableCompression
}

func closeResponseBody(body io.Closer) {
	if err := body.Close(); err != nil {
		return
	}
}
