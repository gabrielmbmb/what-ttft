package groq

import (
	"net/http"
	"os"
	"strings"
)

// DefaultBaseURL is the public Groq OpenAI-compatible API base URL used when Config.BaseURL is empty.
const DefaultBaseURL = "https://api.groq.com/openai/v1"

// API identifies which Groq HTTP API surface the provider should benchmark.
type API string

const (
	// ChatCompletionsAPI selects POST /chat/completions and is the default when Config.API is empty.
	ChatCompletionsAPI API = "chat-completions"

	// ResponsesAPI selects POST /responses, which Groq offers in beta.
	ResponsesAPI API = "responses"
)

// ServiceTier identifies the Groq processing tier requested for a benchmark request.
type ServiceTier string

const (
	// ServiceTierOnDemand requests Groq standard on-demand processing and is Groq's default.
	ServiceTierOnDemand ServiceTier = "on_demand"

	// ServiceTierFlex requests Groq flex processing.
	ServiceTierFlex ServiceTier = "flex"

	// ServiceTierAuto lets Groq automatically choose the processing tier.
	ServiceTierAuto ServiceTier = "auto"

	// ServiceTierPerformance requests Groq performance-tier processing where available.
	ServiceTierPerformance ServiceTier = "performance"
)

// Config configures a Groq streaming provider.
type Config struct {
	// API selects the Groq API surface to benchmark; empty defaults to ChatCompletionsAPI and ResponsesAPI selects the beta Responses API.
	API API

	// BaseURL is the Groq API base URL without the endpoint suffix; empty uses https://api.groq.com/openai/v1 and must not contain credentials.
	BaseURL string

	// APIKey is the bearer token used for Authorization; empty means APIKeyEnv is consulted and the value must never be logged.
	APIKey string

	// APIKeyEnv is the environment variable name used to resolve APIKey when APIKey is empty; empty means no environment lookup is performed.
	APIKeyEnv string

	// Model is the Groq model identifier sent in each request; empty is invalid for StreamChat.
	Model string

	// ServiceTier is the optional Groq service_tier request value; empty omits the field, otherwise it must be on_demand, flex, auto, or performance.
	ServiceTier ServiceTier

	// UseLegacyMaxTokens sends max_tokens instead of max_completion_tokens for the Chat Completions API; Responses always uses max_output_tokens.
	UseLegacyMaxTokens bool

	// IncludeUsage sends Chat Completions stream_options.include_usage=true when true; Responses usage is captured from terminal response events instead.
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
		return ChatCompletionsAPI
	}

	return c.API
}

func (c Config) chatCompletionsEndpointURL() string {
	return c.baseURL() + "/chat/completions"
}

func (c Config) responsesEndpointURL() string {
	return c.baseURL() + "/responses"
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

// ValidAPI reports whether api is an accepted Groq API surface or empty.
func ValidAPI(api API) bool {
	switch api {
	case "", ChatCompletionsAPI, ResponsesAPI:
		return true
	default:
		return false
	}
}

// ValidServiceTier reports whether tier is an accepted Groq service tier or empty.
func ValidServiceTier(tier ServiceTier) bool {
	switch tier {
	case "", ServiceTierOnDemand, ServiceTierFlex, ServiceTierAuto, ServiceTierPerformance:
		return true
	default:
		return false
	}
}
