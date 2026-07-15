// Package together implements a Together AI Chat Completions streaming provider.
//
// The Together AI chat completions API is OpenAI-compatible, so this adapter mirrors the
// OpenAI Chat Completions streaming path. Together streams a nullable reasoning field in each
// delta, which this adapter records separately from user-visible output so it never influences
// TTFT. Together does not expose server-side timing, prompt-cache counters, or service tiers.
package together
