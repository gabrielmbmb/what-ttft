package whatttft

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

const cacheBustNonceLocationPrefix = "prefix"

// PromptPlan describes the final prompt text and cache metadata for one request attempt.
type PromptPlan struct {
	// Prompt is the final user prompt after cache-mode mutation; empty means the scenario prompt was empty and the value may contain sensitive prompt text.
	Prompt string

	// PromptHash is the SHA-256 hex digest of Prompt; empty means BuildPromptPlan was not used or hashing was not performed.
	PromptHash string

	// CacheMode is the normalized cache mode requested for this prompt; empty is normalized to CacheBust by BuildPromptPlan.
	CacheMode CacheMode

	// Nonce is the cache-busting nonce inserted into Prompt; empty means no nonce was inserted and the value is not a secret.
	Nonce string

	// NonceLocation describes where Nonce was inserted, such as "prefix"; empty means no nonce was inserted.
	NonceLocation string
}

// BuildPromptPlan applies RunConfig cache-mode prompt mutation for one request and hashes the final prompt.
func BuildPromptPlan(cfg RunConfig, requestIndex int, warmup bool) PromptPlan {
	mode := cfg.CacheMode
	if mode == "" {
		mode = CacheBust
	}

	plan := PromptPlan{
		Prompt:    cfg.Scenario.Prompt,
		CacheMode: mode,
	}

	if mode == CacheBust {
		plan.Nonce = newCacheBustNonce(requestIndex, warmup)
		plan.NonceLocation = cacheBustNonceLocationPrefix
		plan.Prompt = fmt.Sprintf("Benchmark nonce: %s. Do not mention this nonce.\n\n%s", plan.Nonce, plan.Prompt)
	}

	plan.PromptHash = hashPrompt(plan.Prompt)
	return plan
}

func hashPrompt(prompt string) string {
	sum := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(sum[:])
}

func newCacheBustNonce(requestIndex int, warmup bool) string {
	phase := "measured"
	if warmup {
		phase = "warmup"
	}

	var randomBytes [12]byte
	if _, err := rand.Read(randomBytes[:]); err == nil {
		return fmt.Sprintf("%s-%06d-%s", phase, requestIndex, hex.EncodeToString(randomBytes[:]))
	}

	fallbackInput := fmt.Sprintf("%d:%d:%t", time.Now().UnixNano(), requestIndex, warmup)
	fallback := sha256.Sum256([]byte(fallbackInput))
	return fmt.Sprintf("%s-%06d-%s", phase, requestIndex, hex.EncodeToString(fallback[:12]))
}
