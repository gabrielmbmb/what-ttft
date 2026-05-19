package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/gabrielmbmb/what-ttft/internal/httptracecap"
	"github.com/gabrielmbmb/what-ttft/internal/sse"
	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

const (
	// maxErrorBodyBytes bounds non-2xx response bodies included in APIError diagnostics.
	maxErrorBodyBytes = 64 * 1024

	// minResponsesMaxOutputTokens is the OpenAPI minimum for Responses max_output_tokens when the field is set.
	minResponsesMaxOutputTokens = 16

	// providerName is the normalized provider identifier emitted in request records.
	providerName = "openai"

	// streamProtocolSSE identifies data-only Server-Sent Events streams.
	streamProtocolSSE = "sse"

	// textModality identifies user-visible text output deltas.
	textModality = "text"

	// openAIProcessingMSHeader is OpenAI's provider-reported request processing duration header.
	openAIProcessingMSHeader = "openai-processing-ms"

	// responseOutputTextDeltaEvent is the Responses stream event carrying visible output text.
	responseOutputTextDeltaEvent = "response.output_text.delta"

	// responseRefusalDeltaEvent is the Responses stream event carrying visible refusal text.
	responseRefusalDeltaEvent = "response.refusal.delta"
)

// Provider streams OpenAI-compatible requests over direct HTTP.
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
		return "openai API error"
	}
	if e.BodySnippet == "" {
		return fmt.Sprintf("openai request failed: %s", e.Status)
	}

	return fmt.Sprintf("openai request failed: %s: %s", e.Status, e.BodySnippet)
}

// New creates an OpenAI-compatible streaming provider.
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

// Name returns the normalized OpenAI-compatible provider name.
func (p *Provider) Name() string {
	return providerName
}

// Model returns the configured model identifier.
func (p *Provider) Model() string {
	return p.cfg.Model
}

// Capabilities returns OpenAI streaming capabilities exposed by this adapter.
func (p *Provider) Capabilities() whatttft.ProviderCapabilities {
	api := p.cfg.api()
	supportsUsage := true
	if api == ChatCompletionsAPI {
		supportsUsage = p.cfg.IncludeUsage
	}

	return whatttft.ProviderCapabilities{
		StreamingProtocol:     streamProtocolSSE,
		SupportsChat:          true,
		SupportsUsageInStream: supportsUsage,
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

	switch p.cfg.api() {
	case ResponsesAPI:
		return p.streamResponses(ctx, req, obs, apiKey)
	case ChatCompletionsAPI:
		return p.streamChatCompletions(ctx, req, obs, apiKey)
	default:
		return fmt.Errorf("unsupported openai API %q", p.cfg.api())
	}
}

func (p *Provider) validateInputs(obs whatttft.ProviderObserver) (string, error) {
	if obs == nil {
		return "", errors.New("openai provider observer is nil")
	}
	if p.cfg.Model == "" {
		return "", errors.New("openai model is required")
	}
	if !validAPI(p.cfg.api()) {
		return "", fmt.Errorf("unsupported openai API %q", p.cfg.api())
	}
	if !validServiceTier(p.cfg.ServiceTier) {
		return "", fmt.Errorf("unsupported openai service tier %q", p.cfg.ServiceTier)
	}
	if p.cfg.api() == ResponsesAPI && p.cfg.UseLegacyMaxTokens {
		return "", errors.New("legacy max_tokens is only supported with openai API chat-completions")
	}

	apiKey := p.cfg.apiKey()
	if apiKey == "" {
		return "", errors.New("openai API key is required")
	}

	return apiKey, nil
}

func (p *Provider) streamResponses(ctx context.Context, req whatttft.ProviderRequest, obs whatttft.ProviderObserver, apiKey string) error {
	responseReq, err := p.responseRequest(req)
	if err != nil {
		return err
	}
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
		p.observeHTTP(obs, capture, "")
		return apiErr
	}

	observedServiceTier, err := p.readResponsesStream(resp, req.RequestID, obs)
	p.observeHTTP(obs, capture, observedServiceTier)
	if err != nil {
		return err
	}

	return nil
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
		p.observeHTTP(obs, capture, "")
		return apiErr
	}

	observedServiceTier, err := p.readChatCompletionsStream(resp, req.RequestID, obs)
	p.observeHTTP(obs, capture, observedServiceTier)
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
		return nil, capture, fmt.Errorf("create openai request: %w", err)
	}
	p.setHeaders(httpReq, apiKey)

	obs.Mark(whatttft.EventRequestStart)
	resp, err := p.client.Do(httpReq)
	if err != nil {
		p.observeHTTP(obs, capture, "")
		return nil, capture, fmt.Errorf("send openai request: %w", err)
	}

	obs.Mark(whatttft.EventHeadersReceived)
	capture.ObserveResponse(resp)
	if processingMS, ok := parseOpenAIProcessingMS(resp.Header); ok {
		capture.ObserveProviderProcessingMS(processingMS)
	}
	p.observeHTTP(obs, capture, "")

	return resp, capture, nil
}

func (p *Provider) responseRequest(req whatttft.ProviderRequest) (responseRequest, error) {
	if req.Scenario.MaxOutputTokens > 0 && req.Scenario.MaxOutputTokens < minResponsesMaxOutputTokens {
		return responseRequest{}, fmt.Errorf("responses max_output_tokens must be at least %d when set", minResponsesMaxOutputTokens)
	}

	includeObfuscation := false
	responseReq := responseRequest{
		Model:       p.cfg.Model,
		Input:       req.Prompt,
		Stream:      true,
		Temperature: req.Scenario.Temperature,
		TopP:        req.Scenario.TopP,
		StreamOptions: &responseStreamOptions{
			IncludeObfuscation: &includeObfuscation,
		},
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

	return responseReq, nil
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
	if p.cfg.Organization != "" {
		req.Header.Set("OpenAI-Organization", p.cfg.Organization)
	}
	if p.cfg.Project != "" {
		req.Header.Set("OpenAI-Project", p.cfg.Project)
	}
}

func (p *Provider) observeHTTP(obs whatttft.ProviderObserver, capture *httptracecap.Capture, observedServiceTier ServiceTier) {
	record := capture.Record()
	record.RequestedServiceTier = string(p.cfg.ServiceTier)
	record.ObservedServiceTier = string(observedServiceTier)
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

		nextObservedServiceTier := serviceTierFromResponse(responseEvent.Response)
		if nextObservedServiceTier != "" {
			observedServiceTier = nextObservedServiceTier
		}

		switch responseEvent.Type {
		case responseOutputTextDeltaEvent, responseRefusalDeltaEvent:
			if responseEvent.Delta == "" {
				continue
			}
			obs.MarkFirst(whatttft.EventFirstOutputDelta)
			obs.MarkLast(whatttft.EventLastOutputDelta)
			obs.OnOutputDelta(whatttft.OutputDelta{
				RequestID: requestID,
				Index:     outputIndex,
				Text:      responseEvent.Delta,
				Modality:  textModality,
				Visible:   true,
			})
			outputIndex++
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
			closeResponseBody(resp.Body)
			return observedServiceTier, responseFailureError(responseEvent.Response)
		case "error":
			closeResponseBody(resp.Body)
			return observedServiceTier, responseEventError(responseEvent)
		}
	}
}

func (p *Provider) readChatCompletionsStream(resp *http.Response, requestID string, obs whatttft.ProviderObserver) (ServiceTier, error) {
	parser := sse.New(resp.Body)
	streamIndex := 0
	outputIndex := 0
	var observedServiceTier ServiceTier

	for {
		event, err := parser.Next()
		if errors.Is(err, io.EOF) {
			if closeErr := resp.Body.Close(); closeErr != nil {
				return observedServiceTier, fmt.Errorf("close chat completion stream: %w", closeErr)
			}
			obs.Mark(whatttft.EventBodyEOF)
			return observedServiceTier, nil
		}
		if err != nil {
			closeResponseBody(resp.Body)
			return observedServiceTier, fmt.Errorf("read SSE event: %w", err)
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
			return observedServiceTier, fmt.Errorf("decode chat completion chunk: %w", err)
		}
		if chunk.ServiceTier != "" {
			observedServiceTier = chunk.ServiceTier
		}

		if chunk.Usage != nil {
			obs.OnUsage(normalizeChatUsage(chunk.Usage))
			obs.OnCache(normalizeChatCache(chunk.Usage))
		}

		for _, choice := range chunk.Choices {
			text := choice.Delta.Content
			if text == "" {
				text = choice.Delta.Refusal
			}

			if text == "" {
				if choice.Delta.Role != "" || choice.FinishReason != "" {
					obs.OnOutputDelta(whatttft.OutputDelta{
						RequestID:    requestID,
						Index:        outputIndex,
						Role:         choice.Delta.Role,
						Modality:     textModality,
						Visible:      false,
						FinishReason: choice.FinishReason,
					})
					outputIndex++
				}
				if choice.FinishReason != "" {
					obs.OnFinish(whatttft.FinishEvent{RequestID: requestID, FinishReason: choice.FinishReason})
				}
				continue
			}

			obs.MarkFirst(whatttft.EventFirstOutputDelta)
			obs.MarkLast(whatttft.EventLastOutputDelta)
			obs.OnOutputDelta(whatttft.OutputDelta{
				RequestID:    requestID,
				Index:        outputIndex,
				Text:         text,
				Role:         choice.Delta.Role,
				Modality:     textModality,
				Visible:      true,
				FinishReason: choice.FinishReason,
			})
			outputIndex++

			if choice.FinishReason != "" {
				obs.OnFinish(whatttft.FinishEvent{RequestID: requestID, FinishReason: choice.FinishReason})
			}
		}
	}
}

func parseOpenAIProcessingMS(header http.Header) (float64, bool) {
	value := strings.TrimSpace(header.Get(openAIProcessingMSHeader))
	if value == "" {
		return 0, false
	}

	processingMS, err := strconv.ParseFloat(value, 64)
	if err != nil || processingMS < 0 || math.IsNaN(processingMS) || math.IsInf(processingMS, 0) {
		return 0, false
	}

	return processingMS, true
}

func normalizeChatUsage(raw *usage) whatttft.ProviderUsage {
	return whatttft.ProviderUsage{
		PromptTokens:     intPointer(raw.PromptTokens),
		CompletionTokens: intPointer(raw.CompletionTokens),
		TotalTokens:      intPointer(raw.TotalTokens),
		Source:           "provider-reported",
	}
}

func normalizeChatCache(raw *usage) whatttft.CacheRecord {
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

func observeResponsesUsage(obs whatttft.ProviderObserver, raw *responseUsage) {
	if raw == nil {
		return
	}

	usage := whatttft.ProviderUsage{
		PromptTokens:     intPointer(raw.InputTokens),
		CompletionTokens: intPointer(raw.OutputTokens),
		TotalTokens:      intPointer(raw.TotalTokens),
		Source:           "provider-reported",
	}
	if raw.OutputTokensDetails != nil {
		usage.Extra = map[string]any{"reasoning_tokens": raw.OutputTokensDetails.ReasoningTokens}
	}
	obs.OnUsage(usage)

	if raw.InputTokensDetails == nil {
		return
	}
	cachedTokens := raw.InputTokensDetails.CachedTokens
	hit := cachedTokens > 0
	obs.OnCache(whatttft.CacheRecord{
		Hit:                &hit,
		PromptCachedTokens: &cachedTokens,
	})
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
