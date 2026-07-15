package together

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
	providerName = "together"

	// streamProtocolSSE identifies data-only Server-Sent Events streams.
	streamProtocolSSE = "sse"

	// textModality identifies user-visible text output deltas.
	textModality = "text"

	// reasoningModality identifies hidden reasoning output deltas that must never drive TTFT.
	reasoningModality = "reasoning"
)

// Provider streams Together AI Chat Completions requests over direct HTTP.
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
		return "together API error"
	}
	if e.BodySnippet == "" {
		return fmt.Sprintf("together request failed: %s", e.Status)
	}

	return fmt.Sprintf("together request failed: %s: %s", e.Status, e.BodySnippet)
}

// New creates a Together AI Chat Completions streaming provider.
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

// Name returns the normalized Together provider name.
func (p *Provider) Name() string {
	return providerName
}

// Model returns the configured model identifier.
func (p *Provider) Model() string {
	return p.cfg.Model
}

// Capabilities returns Together streaming capabilities exposed by this adapter.
func (p *Provider) Capabilities() whatttft.ProviderCapabilities {
	return whatttft.ProviderCapabilities{
		StreamingProtocol: streamProtocolSSE,
		SupportsChat:      true,
		// Together streams a usage chunk near the end of the stream by default.
		SupportsUsageInStream: true,
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
		p.observeHTTP(obs, capture)
		return apiErr
	}

	streamErr := p.readChatCompletionsStream(resp, req.RequestID, obs)
	p.observeHTTP(obs, capture)
	if streamErr != nil {
		return streamErr
	}

	return nil
}

func (p *Provider) validateInputs(obs whatttft.ProviderObserver) (string, error) {
	if obs == nil {
		return "", errors.New("together provider observer is nil")
	}
	if p.cfg.Model == "" {
		return "", errors.New("together model is required")
	}

	apiKey := p.cfg.apiKey()
	if apiKey == "" {
		return "", errors.New("together API key is required")
	}

	return apiKey, nil
}

func (p *Provider) doStreamingRequest(ctx context.Context, endpoint string, body []byte, apiKey string, obs whatttft.ProviderObserver) (*http.Response, *httptracecap.Capture, error) {
	capture := httptracecap.NewCapture(httptracecap.CaptureConfig{CompressionDisabled: p.compressionDisabled})
	tracedCtx := httptracecap.WithTrace(ctx, obs, capture)
	httpReq, err := http.NewRequestWithContext(tracedCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, capture, fmt.Errorf("create together request: %w", err)
	}
	p.setHeaders(httpReq, apiKey)

	obs.Mark(whatttft.EventRequestStart)
	resp, err := p.client.Do(httpReq)
	if err != nil {
		p.observeHTTP(obs, capture)
		return nil, capture, fmt.Errorf("send together request: %w", err)
	}

	obs.Mark(whatttft.EventHeadersReceived)
	capture.ObserveResponse(resp)
	p.observeHTTP(obs, capture)

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

func (p *Provider) observeHTTP(obs whatttft.ProviderObserver, capture *httptracecap.Capture) {
	obs.OnHTTP(capture.Record())
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

func (p *Provider) readChatCompletionsStream(resp *http.Response, requestID string, obs whatttft.ProviderObserver) error {
	parser := sse.New(resp.Body)
	streamIndex := 0
	outputIndex := 0

	for {
		event, err := parser.Next()
		if errors.Is(err, io.EOF) {
			if closeErr := resp.Body.Close(); closeErr != nil {
				return fmt.Errorf("close chat completion stream: %w", closeErr)
			}
			obs.Mark(whatttft.EventBodyEOF)
			return nil
		}
		if err != nil {
			closeResponseBody(resp.Body)
			return fmt.Errorf("read SSE event: %w", err)
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
			return fmt.Errorf("decode chat completion chunk: %w", err)
		}

		if chunk.Usage != nil {
			obs.OnUsage(normalizeUsage(chunk.Usage))
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

func normalizeUsage(raw *usage) whatttft.ProviderUsage {
	return whatttft.ProviderUsage{
		PromptTokens:     intPointer(raw.PromptTokens),
		CompletionTokens: intPointer(raw.CompletionTokens),
		TotalTokens:      intPointer(raw.TotalTokens),
		Source:           "provider-reported",
	}
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
