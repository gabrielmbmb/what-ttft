package cerebras

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
	providerName = "cerebras"

	// streamProtocolSSE identifies data-only Server-Sent Events streams.
	streamProtocolSSE = "sse"

	// textModality identifies user-visible text output deltas.
	textModality = "text"

	// reasoningModality identifies hidden reasoning output deltas that must never drive TTFT.
	reasoningModality = "reasoning"

	// secondsToMS converts provider-reported seconds into the milliseconds used across the benchmark schema.
	secondsToMS = 1000.0
)

// Provider streams Cerebras Chat Completions requests over direct HTTP.
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
		return "cerebras API error"
	}
	if e.BodySnippet == "" {
		return fmt.Sprintf("cerebras request failed: %s", e.Status)
	}

	return fmt.Sprintf("cerebras request failed: %s: %s", e.Status, e.BodySnippet)
}

// New creates a Cerebras Chat Completions streaming provider.
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

// Name returns the normalized Cerebras provider name.
func (p *Provider) Name() string {
	return providerName
}

// Model returns the configured model identifier.
func (p *Provider) Model() string {
	return p.cfg.Model
}

// Capabilities returns Cerebras streaming capabilities exposed by this adapter.
func (p *Provider) Capabilities() whatttft.ProviderCapabilities {
	return whatttft.ProviderCapabilities{
		StreamingProtocol:     streamProtocolSSE,
		SupportsChat:          true,
		SupportsUsageInStream: p.cfg.IncludeUsage,
		SupportsPromptCache:   true,
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

func (p *Provider) validateInputs(obs whatttft.ProviderObserver) (string, error) {
	if obs == nil {
		return "", errors.New("cerebras provider observer is nil")
	}
	if p.cfg.Model == "" {
		return "", errors.New("cerebras model is required")
	}
	if !ValidServiceTier(p.cfg.ServiceTier) {
		return "", fmt.Errorf("unsupported cerebras service tier %q", p.cfg.ServiceTier)
	}

	apiKey := p.cfg.apiKey()
	if apiKey == "" {
		return "", errors.New("cerebras API key is required")
	}

	return apiKey, nil
}

func (p *Provider) doStreamingRequest(ctx context.Context, endpoint string, body []byte, apiKey string, obs whatttft.ProviderObserver) (*http.Response, *httptracecap.Capture, error) {
	capture := httptracecap.NewCapture(httptracecap.CaptureConfig{CompressionDisabled: p.compressionDisabled})
	tracedCtx := httptracecap.WithTrace(ctx, obs, capture)
	httpReq, err := http.NewRequestWithContext(tracedCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, capture, fmt.Errorf("create cerebras request: %w", err)
	}
	p.setHeaders(httpReq, apiKey)

	obs.Mark(whatttft.EventRequestStart)
	resp, err := p.client.Do(httpReq)
	if err != nil {
		p.observeHTTP(obs, capture, "", nil)
		return nil, capture, fmt.Errorf("send cerebras request: %w", err)
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
		Model:           p.cfg.Model,
		Messages:        messages,
		Stream:          true,
		Temperature:     req.Scenario.Temperature,
		TopP:            req.Scenario.TopP,
		Stop:            req.Scenario.Stop,
		Seed:            req.Scenario.Seed,
		ReasoningEffort: req.Scenario.ReasoningEffort,
		ServiceTier:     p.cfg.ServiceTier,
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
		if tier := observedTier(chunk); tier != "" {
			observedServiceTier = tier
		}
		if chunk.TimeInfo != nil {
			timing = normalizeTimeInfo(chunk.TimeInfo)
		}

		if chunk.Usage != nil {
			obs.OnUsage(normalizeUsage(chunk.Usage))
			obs.OnCache(normalizeCache(chunk.Usage))
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

func observedTier(chunk chatCompletionChunk) ServiceTier {
	if chunk.ServiceTierUsed != "" {
		return chunk.ServiceTierUsed
	}

	return chunk.ServiceTier
}

func normalizeUsage(raw *usage) whatttft.ProviderUsage {
	result := whatttft.ProviderUsage{
		PromptTokens:     intPointer(raw.PromptTokens),
		CompletionTokens: intPointer(raw.CompletionTokens),
		TotalTokens:      intPointer(raw.TotalTokens),
		Source:           "provider-reported",
	}

	extra := map[string]any{}
	if raw.ImageTokens > 0 {
		extra["image_tokens"] = raw.ImageTokens
	}
	if raw.CompletionTokensDetails != nil {
		if raw.CompletionTokensDetails.ReasoningTokens > 0 {
			extra["reasoning_tokens"] = raw.CompletionTokensDetails.ReasoningTokens
		}
		if raw.CompletionTokensDetails.AcceptedPredictionTokens > 0 {
			extra["accepted_prediction_tokens"] = raw.CompletionTokensDetails.AcceptedPredictionTokens
		}
		if raw.CompletionTokensDetails.RejectedPredictionTokens > 0 {
			extra["rejected_prediction_tokens"] = raw.CompletionTokensDetails.RejectedPredictionTokens
		}
	}
	if len(extra) > 0 {
		result.Extra = extra
	}

	return result
}

func normalizeCache(raw *usage) whatttft.CacheRecord {
	if raw.PromptTokensDetails == nil {
		return whatttft.CacheRecord{}
	}

	cachedTokens := raw.PromptTokensDetails.CachedTokens
	hit := cachedTokens > 0

	return whatttft.CacheRecord{
		Hit:                &hit,
		PromptCachedTokens: &cachedTokens,
	}
}

func normalizeTimeInfo(raw *timeInfo) *whatttft.ServerTiming {
	return &whatttft.ServerTiming{
		QueueTimeMS:      msPointer(raw.QueueTime),
		PromptTimeMS:     msPointer(raw.PromptTime),
		CompletionTimeMS: msPointer(raw.CompletionTime),
		TotalTimeMS:      msPointer(raw.TotalTime),
	}
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
