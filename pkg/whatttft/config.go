package whatttft

// CacheMode describes how benchmark requests should interact with provider prompt/KV caches.
type CacheMode string

const (
	// CacheBust avoids provider prompt/KV cache hits for cold comparisons and must not be summarized with cached modes.
	CacheBust CacheMode = "cache-bust"

	// CacheReuse intentionally reuses identical cacheable prompt text to measure cached latency and must not be summarized with cache-busted requests.
	CacheReuse CacheMode = "cache-reuse"

	// ProviderExplicitCache uses provider-specific cache controls or context-cache APIs and must be summarized separately from implicit cache modes.
	ProviderExplicitCache CacheMode = "provider-explicit-cache"

	// CacheUnknown records requests whose provider prompt/KV cache behavior cannot be controlled or observed.
	CacheUnknown CacheMode = "unknown"
)

// ConnectionMode describes whether benchmark requests should reuse HTTP connections.
type ConnectionMode string

const (
	// WarmConnections reuses HTTP transports and idle connections when possible and must not be summarized with cold connection requests.
	WarmConnections ConnectionMode = "warm"

	// ColdConnections avoids HTTP keepalive reuse for each request, while still allowing OS-level DNS, TCP, or TLS session caches.
	ColdConnections ConnectionMode = "cold"
)

// RunConfig configures one benchmark run for a single provider and scenario.
type RunConfig struct {
	// Scenario is the prompt shape and model parameters used for every request in the run; zero value is invalid because the prompt is empty.
	Scenario Scenario

	// WarmupRequests is the count of warmup requests to run before measured requests; zero means no warmup phase.
	WarmupRequests int

	// MeasuredRequests is the count of measured requests to include in default summaries; zero means no measured samples.
	MeasuredRequests int

	// Concurrency is the maximum count of in-flight requests; zero is treated as one by runners that validate this config.
	Concurrency int

	// CacheMode is the requested prompt/KV cache behavior for every request in this run; summaries must not mix different cache modes.
	CacheMode CacheMode

	// ConnectionMode is the requested HTTP connection reuse behavior; summaries must not mix different connection modes.
	ConnectionMode ConnectionMode

	// TargetID is an optional stable target identifier for multi-target benchmarks; empty means no target dimension is recorded and the value must not contain secrets.
	TargetID string

	// TargetName is an optional human-readable target label for reports; empty means no separate target label is recorded and the value must not contain secrets.
	TargetName string

	// RequestIDPrefix is an optional prefix prepended to generated request IDs; empty preserves req-000000 IDs, non-empty values must be stable and contain no secrets.
	RequestIDPrefix string

	// OutputDir is the filesystem directory for reports; empty means the report writer or CLI may generate a unique directory under its default output root.
	OutputDir string

	// SaveChunks controls whether generated content chunks are written to chunk reports; false omits chunk files because chunks may contain sensitive output.
	SaveChunks bool
}
