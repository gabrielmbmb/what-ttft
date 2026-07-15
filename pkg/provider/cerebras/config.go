package cerebras

import (
	"net/http"
	"os"
	"strings"
)

// DefaultBaseURL is the public Cerebras Inference API base URL used when Config.BaseURL is empty.
const DefaultBaseURL = "https://api.cerebras.ai/v1"

// ServiceTier identifies the Cerebras request prioritization tier requested for a benchmark request.
type ServiceTier string

const (
	// ServiceTierAuto requests that Cerebras automatically use the highest available service tier.
	ServiceTierAuto ServiceTier = "auto"

	// ServiceTierDefault requests Cerebras standard priority processing.
	ServiceTierDefault ServiceTier = "default"

	// ServiceTierFlex requests Cerebras lowest priority (flex) processing.
	ServiceTierFlex ServiceTier = "flex"

	// ServiceTierPriority requests Cerebras highest priority processing; only available for dedicated endpoints.
	ServiceTierPriority ServiceTier = "priority"
)

// Config configures a Cerebras Chat Completions streaming provider.
type Config struct {
	// BaseURL is the Cerebras API base URL without the endpoint suffix; empty uses https://api.cerebras.ai/v1 and must not contain credentials.
	BaseURL string

	// APIKey is the bearer token used for Authorization; empty means APIKeyEnv is consulted and the value must never be logged.
	APIKey string

	// APIKeyEnv is the environment variable name used to resolve APIKey when APIKey is empty; empty means no environment lookup is performed.
	APIKeyEnv string

	// Model is the Cerebras model identifier sent in each request, such as gpt-oss-120b; empty is invalid for StreamChat.
	Model string

	// ServiceTier is the optional Cerebras service_tier request value; empty omits the field, otherwise it must be auto, default, flex, or priority.
	ServiceTier ServiceTier

	// UseLegacyMaxTokens sends max_tokens instead of max_completion_tokens; Cerebras accepts max_completion_tokens, so this is only for compatibility experiments and defaults to false.
	UseLegacyMaxTokens bool

	// IncludeUsage sends stream_options.include_usage=true when true so the terminal chunk carries token usage; defaults to false at the struct level and is enabled by callers that need stream usage.
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

// ValidServiceTier reports whether tier is an accepted Cerebras service tier or empty.
func ValidServiceTier(tier ServiceTier) bool {
	switch tier {
	case "", ServiceTierAuto, ServiceTierDefault, ServiceTierFlex, ServiceTierPriority:
		return true
	default:
		return false
	}
}
