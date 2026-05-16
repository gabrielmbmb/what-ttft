package openai

import (
	"net/http"
	"os"
	"strings"
)

const defaultBaseURL = "https://api.openai.com/v1"

// Config configures an OpenAI-compatible Chat Completions streaming provider.
type Config struct {
	// BaseURL is the provider API base URL without the endpoint suffix; empty uses https://api.openai.com/v1 and must not contain credentials.
	BaseURL string

	// APIKey is the bearer token used for Authorization; empty means APIKeyEnv is consulted and the value must never be logged.
	APIKey string //nolint:gosec // Config intentionally accepts API keys so callers can authenticate requests.

	// APIKeyEnv is the environment variable name used to resolve APIKey when APIKey is empty; empty means no environment lookup is performed.
	APIKeyEnv string

	// Organization is the optional OpenAI organization header value; empty means no OpenAI-Organization header is sent and it must not contain secrets.
	Organization string

	// Project is the optional OpenAI project header value; empty means no OpenAI-Project header is sent and it must not contain secrets.
	Project string

	// Model is the provider model identifier sent in each request; empty is invalid for StreamChat.
	Model string

	// UseLegacyMaxTokens sends max_tokens instead of max_completion_tokens for OpenAI-compatible providers that require the legacy field.
	UseLegacyMaxTokens bool

	// IncludeUsage sends stream_options.include_usage=true when true; false means stream usage chunks are not explicitly requested.
	IncludeUsage bool

	// HTTPClient is the optional client used for requests; nil creates a benchmark-oriented client with compression disabled.
	HTTPClient *http.Client
}

func (c Config) baseURL() string {
	if c.BaseURL == "" {
		return defaultBaseURL
	}

	return strings.TrimRight(c.BaseURL, "/")
}

func (c Config) endpointURL() string {
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
