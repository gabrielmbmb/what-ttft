// Package cerebras implements a Cerebras Inference Chat Completions streaming provider.
//
// The Cerebras chat completions API is OpenAI-compatible, so this adapter mirrors
// the OpenAI Chat Completions streaming path but additionally captures Cerebras-specific
// signals that matter for latency benchmarking: the server-reported time_info breakdown
// (queue, prompt, completion, total) and streamed reasoning deltas, which are recorded
// separately from user-visible output so they never influence TTFT.
package cerebras
