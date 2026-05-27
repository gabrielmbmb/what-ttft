package tui

import (
	"strings"
	"testing"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

// TestRequestDetailLatencyDistinguishesMissingAndZero verifies detail metrics show observed zero separately from missing values.
func TestRequestDetailLatencyDistinguishesMissingAndZero(t *testing.T) {
	zero := 0.0
	store := newLiveStore()
	record := tuiTestRecord("req-zero", "", 0, 0, nil)
	record.Derived.HTTPTTFBMS = &zero
	record.Derived.HeadersLatencyMS = nil
	record.HTTP.ProviderProcessingMS = &zero
	store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventRunStarted, Provider: "openai", Model: "gpt-zero", TotalRequests: 1, MeasuredRequests: 1})
	store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventRequestFinished, RequestID: record.RequestID, Record: &record})

	content := renderRequestDetail(store, requestExplorerState{CursorRequestID: record.RequestID, DetailSection: requestDetailSectionLatency}, 120, 24, defaultTheme())
	for _, want := range []string{"Request detail · latency", "TTFT delta (ttft_delta_ms)=0.0", "E2E delta (e2e_delta_ms)=0.0", "http_ttfb_ms)=0.0", "headers_latency_ms=-", "provider_processing_ms=0.0"} {
		if !strings.Contains(content, want) {
			t.Fatalf("latency detail missing %q:\n%s", want, content)
		}
	}
}

// TestRequestDetailOutcomeRedactsProviderError verifies provider errors and non-200 status are useful but secret-safe.
func TestRequestDetailOutcomeRedactsProviderError(t *testing.T) {
	store := newLiveStore()
	record := tuiTestRecord("req-error", "", 0, 0, &whatttft.ErrorRecord{
		Category:    "http_status",
		Message:     "Authorization Bearer SECRET_API_KEY raw provider body prompt text",
		StatusCode:  500,
		Retryable:   true,
		AtNS:        12_000_000,
		BodySnippet: "api_key=SECRET_API_KEY raw provider body Authorization prompt text",
	})
	record.HTTP.StatusCode = 500
	record.HTTP.Status = "500 Internal Server Error"
	store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventRunStarted, Provider: "openai", Model: "gpt-error", TotalRequests: 1, MeasuredRequests: 1})
	store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventRequestFinished, RequestID: record.RequestID, Record: &record})

	content := renderRequestDetail(store, requestExplorerState{CursorRequestID: record.RequestID, DetailSection: requestDetailSectionOutcome}, 120, 24, defaultTheme())
	for _, want := range []string{"outcome=error", "http_status=500", "http_text=500 Internal Server Error", "error_category=http_status", "retryable=true", "error_at_ms=12.0", "error_message=[redacted]", "body_snippet=[redacted]"} {
		if !strings.Contains(content, want) {
			t.Fatalf("outcome detail missing %q:\n%s", want, content)
		}
	}
	for _, forbidden := range []string{"SECRET_API_KEY", "Authorization", "raw provider body", "prompt text"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("outcome detail rendered forbidden string %q:\n%s", forbidden, content)
		}
	}
}

// TestRequestDetailUsageCacheWarmupBenchRecord verifies bench identity and usage/cache sections include warmup, cache, tier, and target metadata.
func TestRequestDetailUsageCacheWarmupBenchRecord(t *testing.T) {
	cachedTokens := 12
	promptTokens := 20
	completionTokens := 4
	totalTokens := 24
	cacheHit := true
	cacheTTL := int64(3600)
	store := newLiveStore()
	store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventBenchmarkStarted, BenchmarkName: "bench", Targets: []whatttft.RunEventTarget{{TargetID: "target-a", TargetName: "Target A", Provider: "openai", ProviderAPI: "responses", RequestedServiceTier: "priority", Model: "gpt-a", TotalRequests: 1, WarmupRequests: 1}}})
	record := tuiTestRecord("target-a-req-000000", "target-a", 10, 100, nil)
	record.Warmup = true
	record.TargetName = "Target A"
	record.Model = "gpt-a"
	record.RequestedServiceTier = "priority"
	record.ObservedServiceTier = "priority"
	record.PromptTokens = &promptTokens
	record.CompletionTokens = &completionTokens
	record.TotalTokens = &totalTokens
	record.Cache.Hit = &cacheHit
	record.Cache.PromptCachedTokens = &cachedTokens
	record.Cache.CacheTTLSeconds = &cacheTTL
	record.Cache.CacheID = "redacted-cache-id"
	store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventRequestFinished, TargetID: record.TargetID, RequestID: record.RequestID, Record: &record})

	identity := renderRequestDetail(store, requestExplorerState{CursorRequestID: record.RequestID, DetailSection: requestDetailSectionIdentity}, 120, 24, defaultTheme())
	for _, want := range []string{"phase=warmup", "warmup=true", "target_id=target-a", "target_name=Target A", "provider_api=responses", "model=gpt-a", "requested_service_tier=priority", "observed_service_tier=priority"} {
		if !strings.Contains(identity, want) {
			t.Fatalf("identity detail missing %q:\n%s", want, identity)
		}
	}

	usage := renderRequestDetail(store, requestExplorerState{CursorRequestID: record.RequestID, DetailSection: requestDetailSectionUsageCache}, 120, 24, defaultTheme())
	for _, want := range []string{"prompt_tokens=20", "completion_tokens=4", "total_tokens=24", "cache=hit", "cached_tokens=12", "cache_hit=true", "cache_ttl_seconds=3600", "cache_id=redacted-cache-id"} {
		if !strings.Contains(usage, want) {
			t.Fatalf("usage/cache detail missing %q:\n%s", want, usage)
		}
	}
}

// TestRequestDetailTransportAndTimeline verifies reused connection metadata and waterfall timeline details render for successful requests.
func TestRequestDetailTransportAndTimeline(t *testing.T) {
	store := newLiveStore()
	record := tuiTestRecord("req-transport", "", 10, 100, nil)
	record.HTTP.GotConn = true
	record.HTTP.ConnReused = true
	record.HTTP.ConnWasIdle = true
	record.HTTP.ConnIdleTimeNS = 3_000_000
	record.HTTP.Protocol = "HTTP/2.0"
	record.HTTP.TLSVersion = "TLS 1.3"
	record.HTTP.Network = "tcp"
	record.HTTP.RemoteAddr = "127.0.0.1:443"
	record.HTTP.CompressionDisabled = true
	record.Derived.DNSMS = tuiFloatPointer(1)
	record.Derived.TCPConnectMS = tuiFloatPointer(2)
	record.Derived.TLSMS = tuiFloatPointer(3)
	record.Derived.RequestWriteMS = tuiFloatPointer(4)
	store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventRunStarted, Provider: "openai", Model: "gpt-transport", TotalRequests: 1, MeasuredRequests: 1})
	store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventRequestFinished, RequestID: record.RequestID, Record: &record})

	transport := renderRequestDetail(store, requestExplorerState{CursorRequestID: record.RequestID, DetailSection: requestDetailSectionTransport}, 120, 24, defaultTheme())
	for _, want := range []string{"dns_ms=1.0", "tcp_connect_ms=2.0", "tls_ms=3.0", "request_write_ms=4.0", "got_conn=true", "conn=reused", "protocol=HTTP/2.0", "tls_version=TLS 1.3", "compression_disabled=true"} {
		if !strings.Contains(transport, want) {
			t.Fatalf("transport detail missing %q:\n%s", want, transport)
		}
	}

	timeline := renderRequestDetail(store, requestExplorerState{CursorRequestID: record.RequestID, DetailSection: requestDetailSectionTimeline}, 120, 24, defaultTheme())
	for _, want := range []string{"request_start=0.0", "first_response_byte=5.0", "first_output_delta=10.0", "last_output_delta=100.0", "body_eof=120.0", "waterfall ms"} {
		if !strings.Contains(timeline, want) {
			t.Fatalf("timeline detail missing %q:\n%s", want, timeline)
		}
	}
}

// TestRequestDetailOutputUnavailableWithoutCapture verifies output text is not shown unless capture is enabled.
func TestRequestDetailOutputUnavailableWithoutCapture(t *testing.T) {
	store := newLiveStore()
	record := tuiTestRecord("req-output", "", 10, 100, nil)
	store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventRunStarted, Provider: "openai", Model: "gpt-output", TotalRequests: 1, MeasuredRequests: 1})
	store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventRequestFinished, RequestID: record.RequestID, Record: &record})

	content := renderRequestDetail(store, requestExplorerState{CursorRequestID: record.RequestID, DetailSection: requestDetailSectionOutput}, 120, 24, defaultTheme())
	for _, want := range []string{"output_state=disabled", "rerun with --save-chunks"} {
		if !strings.Contains(content, want) {
			t.Fatalf("output detail missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "Hello") || strings.Contains(content, "chunk text") {
		t.Fatalf("output detail rendered generated-content-like text:\n%s", content)
	}
}

// TestRequestDetailSectionNavigationKeys verifies bracket and output keys switch detail sections.
func TestRequestDetailSectionNavigationKeys(t *testing.T) {
	app := newModel(nil)
	record := tuiTestRecord("req-nav", "", 10, 100, nil)
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventRunStarted, TotalRequests: 1, MeasuredRequests: 1}})
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventRequestFinished, RequestID: record.RequestID, Record: &record}})
	app = updateModel(t, app, keyPress("r"))
	app = updateModel(t, app, keyPress("enter"))
	app = updateModel(t, app, keyPress("]"))
	if app.requestExplorer.DetailSection != requestDetailSectionOutcome {
		t.Fatalf("detail section after ] = %d, want outcome", app.requestExplorer.DetailSection)
	}
	app = updateModel(t, app, keyPress("["))
	if app.requestExplorer.DetailSection != requestDetailSectionIdentity {
		t.Fatalf("detail section after [ = %d, want identity", app.requestExplorer.DetailSection)
	}
	app = updateModel(t, app, keyPress("o"))
	if app.requestExplorer.DetailSection != requestDetailSectionOutput {
		t.Fatalf("detail section after o = %d, want output", app.requestExplorer.DetailSection)
	}
}
