// Package groq implements a Groq streaming provider for both the OpenAI-compatible
// Chat Completions API and the beta Responses API exposed under https://api.groq.com/openai/v1.
//
// The adapter records Groq-specific signals useful for latency benchmarking: the server-reported
// timing fields (queue/prompt/completion/total) that Groq returns in the usage payload or an
// x_groq object, and reasoning deltas, which are kept separate from user-visible output so they
// never influence TTFT.
package groq
