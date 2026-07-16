// Package huggingface implements a Hugging Face Inference Providers Chat Completions streaming provider.
//
// The Hugging Face Inference Providers router (https://router.huggingface.co/v1) is an
// OpenAI-compatible chat completions endpoint that forwards requests to backend providers.
// The backend is selected inside the model string as "owner/model:provider" (for example
// "openai/gpt-oss-120b:cerebras"), or omitted for the router's automatic selection. This adapter
// mirrors the OpenAI Chat Completions streaming path and, defensively, records a reasoning delta
// as hidden output when a routed backend emits one so it never influences TTFT. The router does
// not expose server-side timing, prompt-cache counters, or service tiers.
package huggingface
