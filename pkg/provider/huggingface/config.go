package huggingface

import (
	"net/http"
	"os"
	"strings"
)

// DefaultBaseURL is the public Hugging Face Inference Providers router base URL used when Config.BaseURL is empty.
const DefaultBaseURL = "https://router.huggingface.co/v1"

// Config configures a Hugging Face Inference Providers Chat Completions streaming provider.
type Config struct {
	// BaseURL is the router API base URL without the endpoint suffix; empty uses https://router.huggingface.co/v1 and must not contain credentials.
	BaseURL string

	// APIKey is the bearer token used for Authorization; empty means APIKeyEnv is consulted and the value must never be logged.
	APIKey string

	// APIKeyEnv is the environment variable name used to resolve APIKey when APIKey is empty; empty means no environment lookup is performed.
	APIKeyEnv string

	// Model is the Hugging Face model identifier sent in each request, optionally suffixed with a backend such as owner/model:provider; empty is invalid for StreamChat.
	Model string

	// UseLegacyMaxTokens sends max_tokens instead of max_completion_tokens; the router accepts max_tokens, so this defaults to false and is only for compatibility experiments.
	UseLegacyMaxTokens bool

	// IncludeUsage sends stream_options.include_usage=true when true; the router documents include_usage support and emits a final usage chunk before the terminator.
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
