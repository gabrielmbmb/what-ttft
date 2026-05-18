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

	// providerName is the normalized provider identifier emitted in request records.
	providerName = "openai"

	// streamProtocolSSE identifies data-only Server-Sent Events streams.
	streamProtocolSSE = "sse"

	// textModality identifies user-visible text output deltas.
	textModality = "text"

	// openAIProcessingMSHeader is OpenAI's provider-reported request processing duration header.
	openAIProcessingMSHeader = "openai-processing-ms"
)

// Provider streams OpenAI-compatible Chat Completions requests over direct HTTP.
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
		return fmt.Sprintf("openai chat completions request failed: %s", e.Status)
	}

	return fmt.Sprintf("openai chat completions request failed: %s: %s", e.Status, e.BodySnippet)
}

// New creates an OpenAI-compatible Chat Completions streaming provider.
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

// Capabilities returns OpenAI Chat Completions streaming capabilities exposed by this adapter.
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

// StreamChat sends one streaming Chat Completions request and reports standardized benchmark events to obs.
func (p *Provider) StreamChat(ctx context.Context, req whatttft.ProviderRequest, obs whatttft.ProviderObserver) error {
	if obs == nil {
		return errors.New("openai provider observer is nil")
	}
	if p.cfg.Model == "" {
		return errors.New("openai model is required")
	}

	apiKey := p.cfg.apiKey()
	if apiKey == "" {
		return errors.New("openai API key is required")
	}

	body, err := json.Marshal(p.chatRequest(req))
	if err != nil {
		return fmt.Errorf("encode chat completion request: %w", err)
	}

	capture := httptracecap.NewCapture(httptracecap.CaptureConfig{CompressionDisabled: p.compressionDisabled})
	tracedCtx := httptracecap.WithTrace(ctx, obs, capture)
	httpReq, err := http.NewRequestWithContext(tracedCtx, http.MethodPost, p.cfg.endpointURL(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create chat completion request: %w", err)
	}
	p.setHeaders(httpReq, apiKey)

	obs.Mark(whatttft.EventRequestStart)
	//nolint:gosec // Benchmarks intentionally send requests to a caller-configured provider base URL.
	resp, err := p.client.Do(httpReq)
	if err != nil {
		obs.OnHTTP(capture.Record())
		return fmt.Errorf("send chat completion request: %w", err)
	}

	obs.Mark(whatttft.EventHeadersReceived)
	capture.ObserveResponse(resp)
	if processingMS, ok := parseOpenAIProcessingMS(resp.Header); ok {
		capture.ObserveProviderProcessingMS(processingMS)
	}
	obs.OnHTTP(capture.Record())

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		apiErr := p.readAPIError(resp, apiKey)
		obs.OnHTTP(capture.Record())
		return apiErr
	}

	if err := p.readStream(resp, req.RequestID, obs); err != nil {
		obs.OnHTTP(capture.Record())
		return err
	}

	obs.OnHTTP(capture.Record())
	return nil
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
		chatReq.StreamOptions = &streamOptions{IncludeUsage: true}
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

func (p *Provider) readStream(resp *http.Response, requestID string, obs whatttft.ProviderObserver) error {
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
			obs.OnCache(normalizeCache(chunk.Usage))
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

func normalizeUsage(raw *usage) whatttft.ProviderUsage {
	return whatttft.ProviderUsage{
		PromptTokens:     intPointer(raw.PromptTokens),
		CompletionTokens: intPointer(raw.CompletionTokens),
		TotalTokens:      intPointer(raw.TotalTokens),
		Source:           "provider-reported",
	}
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
