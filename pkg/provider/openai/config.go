package openai

import (
	"net/http"
	"os"
	"strings"
)

// DefaultBaseURL is the public OpenAI API base URL used when Config.BaseURL is empty.
const DefaultBaseURL = "https://api.openai.com/v1"

// API identifies which OpenAI HTTP API surface the provider should benchmark.
type API string

const (
	// ResponsesAPI selects POST /v1/responses and is the default when Config.API is empty.
	ResponsesAPI API = "responses"

	// ChatCompletionsAPI selects POST /v1/chat/completions for OpenAI-compatible providers that have not implemented Responses.
	ChatCompletionsAPI API = "chat-completions"
)

// ServiceTier identifies the OpenAI processing tier requested for a benchmark request.
type ServiceTier string

const (
	// ServiceTierAuto requests the project-configured OpenAI tier; OpenAI treats omitted service_tier similarly to auto by default.
	ServiceTierAuto ServiceTier = "auto"

	// ServiceTierDefault requests standard OpenAI pricing and performance for the selected model.
	ServiceTierDefault ServiceTier = "default"

	// ServiceTierFlex requests OpenAI flex processing where supported by the selected model and project.
	ServiceTierFlex ServiceTier = "flex"

	// ServiceTierScale requests OpenAI scale-tier processing where supported by the selected model and project.
	ServiceTierScale ServiceTier = "scale"

	// ServiceTierPriority requests OpenAI priority processing where supported by the selected model and project.
	ServiceTierPriority ServiceTier = "priority"
)

// Config configures an OpenAI-compatible streaming provider.
type Config struct {
	// API selects the OpenAI API surface to benchmark; empty defaults to ResponsesAPI and must be ChatCompletionsAPI only for compatibility endpoints.
	API API

	// BaseURL is the provider API base URL without the endpoint suffix; empty uses https://api.openai.com/v1 and must not contain credentials.
	BaseURL string

	// APIKey is the bearer token used for Authorization; empty means APIKeyEnv is consulted and the value must never be logged.
	APIKey string

	// APIKeyEnv is the environment variable name used to resolve APIKey when APIKey is empty; empty means no environment lookup is performed.
	APIKeyEnv string

	// Organization is the optional OpenAI organization header value; empty means no OpenAI-Organization header is sent and it must not contain secrets.
	Organization string

	// Project is the optional OpenAI project header value; empty means no OpenAI-Project header is sent and it must not contain secrets.
	Project string

	// Model is the provider model identifier sent in each request; empty is invalid for StreamChat.
	Model string

	// ServiceTier is the optional OpenAI service_tier request value; empty omits the field, otherwise it must be auto, default, flex, scale, or priority.
	ServiceTier ServiceTier

	// UseLegacyMaxTokens sends max_tokens instead of max_completion_tokens for OpenAI-compatible Chat Completions providers that require the legacy field.
	UseLegacyMaxTokens bool

	// IncludeUsage sends Chat Completions stream_options.include_usage=true when true; Responses API usage is captured from terminal response events instead.
	IncludeUsage bool

	// HTTPClient is the optional client used for requests; nil creates a benchmark-oriented client with compression disabled.
	HTTPClient *http.Client
}

func (c Config) baseURL() string {
	if c.BaseURL == "" {
		return DefaultBaseURL
	}

	return strings.TrimRight(c.BaseURL, "/")
}

func (c Config) api() API {
	if c.API == "" {
		return ResponsesAPI
	}

	return c.API
}

func (c Config) responsesEndpointURL() string {
	return c.baseURL() + "/responses"
}

func (c Config) chatCompletionsEndpointURL() string {
	return c.baseURL() + "/chat/completions"
}

func (c Config) apiKey() string {
	if c.APIKey != "" {
		return c.APIKey
	}
	if c.APIKeyEnv == "" {
		return ""
	}

	return os.Getenv(c.APIKeyEnv)
}

func validAPI(api API) bool {
	switch api {
	case ResponsesAPI, ChatCompletionsAPI:
		return true
	default:
		return false
	}
}

func validServiceTier(tier ServiceTier) bool {
	switch tier {
	case "", ServiceTierAuto, ServiceTierDefault, ServiceTierFlex, ServiceTierScale, ServiceTierPriority:
		return true
	default:
		return false
	}
}
