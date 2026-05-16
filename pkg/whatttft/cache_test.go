package whatttft

import (
	"strings"
	"testing"
)

// TestBuildPromptPlanCacheBustProducesUniquePromptHashes verifies cache busting mutates each request prompt.
func TestBuildPromptPlanCacheBustProducesUniquePromptHashes(t *testing.T) {
	cfg := RunConfig{
		Scenario:  Scenario{Prompt: "Answer briefly."},
		CacheMode: CacheBust,
	}

	first := BuildPromptPlan(cfg, 0, false)
	second := BuildPromptPlan(cfg, 1, false)

	if first.CacheMode != CacheBust {
		t.Fatalf("cache mode = %q, want %q", first.CacheMode, CacheBust)
	}
	if first.Nonce == "" || second.Nonce == "" {
		t.Fatalf("nonces should be populated: first=%q second=%q", first.Nonce, second.Nonce)
	}
	if first.Nonce == second.Nonce {
		t.Fatalf("nonces should differ, got %q", first.Nonce)
	}
	if first.NonceLocation != cacheBustNonceLocationPrefix {
		t.Fatalf("nonce location = %q, want %q", first.NonceLocation, cacheBustNonceLocationPrefix)
	}
	if !strings.HasPrefix(first.Prompt, "Benchmark nonce: "+first.Nonce) {
		t.Fatalf("prompt %q does not start with nonce prefix %q", first.Prompt, first.Nonce)
	}
	if !strings.HasSuffix(first.Prompt, cfg.Scenario.Prompt) {
		t.Fatalf("prompt %q does not preserve original prompt suffix %q", first.Prompt, cfg.Scenario.Prompt)
	}
	if first.PromptHash == second.PromptHash {
		t.Fatalf("cache-busted prompt hashes should differ, got %q", first.PromptHash)
	}
	if first.PromptHash != hashPrompt(first.Prompt) {
		t.Fatalf("prompt hash = %q, want hash of final prompt", first.PromptHash)
	}
	if strings.Contains(first.PromptHash, first.Nonce) {
		t.Fatalf("prompt hash should not expose raw nonce: hash=%q nonce=%q", first.PromptHash, first.Nonce)
	}
}

// TestBuildPromptPlanDefaultCacheModeBusts verifies an omitted cache mode defaults to cache-bust.
func TestBuildPromptPlanDefaultCacheModeBusts(t *testing.T) {
	plan := BuildPromptPlan(RunConfig{Scenario: Scenario{Prompt: "Hello"}}, 0, false)

	if plan.CacheMode != CacheBust {
		t.Fatalf("cache mode = %q, want default %q", plan.CacheMode, CacheBust)
	}
	if plan.Nonce == "" {
		t.Fatal("default cache-bust mode should insert a nonce")
	}
}

// TestBuildPromptPlanCacheReuseProducesStablePromptHashes verifies cache reuse leaves prompts identical.
func TestBuildPromptPlanCacheReuseProducesStablePromptHashes(t *testing.T) {
	cfg := RunConfig{
		Scenario:  Scenario{Prompt: "Answer briefly."},
		CacheMode: CacheReuse,
	}

	first := BuildPromptPlan(cfg, 0, true)
	second := BuildPromptPlan(cfg, 99, false)

	if first.Prompt != cfg.Scenario.Prompt {
		t.Fatalf("prompt = %q, want original prompt", first.Prompt)
	}
	if second.Prompt != cfg.Scenario.Prompt {
		t.Fatalf("prompt = %q, want original prompt", second.Prompt)
	}
	if first.PromptHash != second.PromptHash {
		t.Fatalf("cache-reuse hashes differ: first=%q second=%q", first.PromptHash, second.PromptHash)
	}
	if first.Nonce != "" || second.Nonce != "" {
		t.Fatalf("cache-reuse should not insert nonces: first=%q second=%q", first.Nonce, second.Nonce)
	}
	if first.NonceLocation != "" || second.NonceLocation != "" {
		t.Fatalf("cache-reuse should not record nonce locations: first=%q second=%q", first.NonceLocation, second.NonceLocation)
	}
}

// TestBuildPromptPlanNoOpCacheModes verifies explicit-cache and unknown modes record the requested mode without mutation.
func TestBuildPromptPlanNoOpCacheModes(t *testing.T) {
	for _, mode := range []CacheMode{ProviderExplicitCache, CacheUnknown} {
		cfg := RunConfig{
			Scenario:  Scenario{Prompt: "Answer briefly."},
			CacheMode: mode,
		}

		plan := BuildPromptPlan(cfg, 7, false)
		if plan.CacheMode != mode {
			t.Fatalf("cache mode = %q, want %q", plan.CacheMode, mode)
		}
		if plan.Prompt != cfg.Scenario.Prompt {
			t.Fatalf("prompt for %q = %q, want original prompt", mode, plan.Prompt)
		}
		if plan.PromptHash != hashPrompt(cfg.Scenario.Prompt) {
			t.Fatalf("hash for %q = %q, want original prompt hash", mode, plan.PromptHash)
		}
		if plan.Nonce != "" {
			t.Fatalf("nonce for %q = %q, want empty", mode, plan.Nonce)
		}
	}
}

// TestBuildPromptPlanWarmupNonceLabelsPhase verifies nonce text records the warmup/measured phase.
func TestBuildPromptPlanWarmupNonceLabelsPhase(t *testing.T) {
	cfg := RunConfig{
		Scenario:  Scenario{Prompt: "Answer briefly."},
		CacheMode: CacheBust,
	}

	warmup := BuildPromptPlan(cfg, 3, true)
	measured := BuildPromptPlan(cfg, 3, false)

	if !strings.HasPrefix(warmup.Nonce, "warmup-000003-") {
		t.Fatalf("warmup nonce = %q, want warmup phase label", warmup.Nonce)
	}
	if !strings.HasPrefix(measured.Nonce, "measured-000003-") {
		t.Fatalf("measured nonce = %q, want measured phase label", measured.Nonce)
	}
}
