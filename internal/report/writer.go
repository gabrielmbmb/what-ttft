// Package report writes benchmark run records and summaries to disk.
package report

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

const (
	benchmarkVersion    = "v0.2.0"
	chunksJSONLName     = "chunks.jsonl"
	defaultOutputRoot   = "runs"
	requestsJSONLName   = "requests.jsonl"
	runJSONName         = "run.json"
	summaryJSONName     = "summary.json"
	summaryMarkdownName = "summary.md"
)

// RunMetadata is the reproducibility metadata written to run.json.
type RunMetadata struct {
	// BenchmarkVersion is the benchmark schema/tool version; empty is filled with the current development version.
	BenchmarkVersion string `json:"benchmark_version"`

	// GitSHA is the best-effort repository commit identifier; empty means it was unavailable or not detected.
	GitSHA string `json:"git_sha,omitempty"`

	// GoVersion is the Go runtime version used by the benchmark process; empty is filled from runtime.Version.
	GoVersion string `json:"go_version"`

	// GOOS is the operating system reported by the Go runtime; empty is filled from runtime.GOOS.
	GOOS string `json:"goos"`

	// GOARCH is the CPU architecture reported by the Go runtime; empty is filled from runtime.GOARCH.
	GOARCH string `json:"goarch"`

	// BenchmarkName is the optional multi-target benchmark name from a YAML config; empty means this was an unnamed or single-target ad-hoc run and it must not contain secrets.
	BenchmarkName string `json:"benchmark_name,omitempty"`

	// ConfigPath is the YAML config path used for a multi-target benchmark; empty means no config file was used and the path is not treated as secret.
	ConfigPath string `json:"config_path,omitempty"`

	// ConfigSHA256 is the SHA-256 hex digest of the YAML config bytes used for a multi-target benchmark; empty means unavailable and the digest contains no secrets by itself.
	ConfigSHA256 string `json:"config_sha256,omitempty"`

	// TargetOrder records how benchmark targets were scheduled, such as serial; empty means unavailable or single-target execution.
	TargetOrder string `json:"target_order,omitempty"`

	// Targets contains per-target metadata for YAML or multi-target benchmarks; nil means no target list was supplied and all secret-bearing fields must be redacted by the caller or writer.
	Targets []RunTargetMetadata `json:"targets,omitempty"`

	// Provider is the normalized provider name for this run; empty means unspecified and it must not contain secrets.
	Provider string `json:"provider"`

	// Model is the provider model identifier for this run; empty means unspecified and it must not contain API keys or credentials.
	Model string `json:"model"`

	// BaseURL is the provider endpoint/base URL with credentials and secret query values redacted; empty means unavailable.
	BaseURL string `json:"base_url,omitempty"`

	// ProviderAPI is the provider API surface requested for this run, such as openai responses or chat-completions; empty means unspecified and it must not contain secrets.
	ProviderAPI string `json:"provider_api,omitempty"`

	// RequestedServiceTier is the provider service tier requested for this run, such as OpenAI default or priority; empty means unset and it must not contain secrets.
	RequestedServiceTier string `json:"requested_service_tier,omitempty"`

	// Scenario is the benchmark scenario configuration; prompt fields may contain sensitive data and are intentionally limited to run.json.
	Scenario whatttft.Scenario `json:"scenario"`

	// RunConfig is the run configuration used for execution; prompt fields may contain sensitive data through Scenario and are intentionally limited to run.json.
	RunConfig whatttft.RunConfig `json:"run_config"`

	// WallStartUnixNano is the wall-clock Unix nanosecond timestamp for run start; zero means unavailable.
	WallStartUnixNano int64 `json:"wall_start_unix_nano"`

	// WallEndUnixNano is the wall-clock Unix nanosecond timestamp for run end; zero means unavailable.
	WallEndUnixNano int64 `json:"wall_end_unix_nano"`

	// Args is the command-line argument vector with secrets redacted by the caller; nil or empty means unavailable.
	Args []string `json:"args,omitempty"`
}

// RunTargetMetadata describes one configured target in run.json for multi-target benchmark reproducibility.
type RunTargetMetadata struct {
	// TargetID is the stable sanitized benchmark target identifier used in request records and summary groups; empty means unavailable and it must not contain secrets.
	TargetID string `json:"target_id"`

	// TargetName is an optional human-readable target label; empty means no separate label was supplied and it must not contain secrets.
	TargetName string `json:"target_name,omitempty"`

	// Provider is the normalized provider name for this target, such as openai; empty means unavailable and it must not contain secrets.
	Provider string `json:"provider"`

	// ProviderAPI is the provider API surface requested for this target, such as responses or chat-completions; empty means unspecified and it must not contain secrets.
	ProviderAPI string `json:"provider_api,omitempty"`

	// RequestedServiceTier is the provider service tier requested for this target, such as OpenAI default or priority; empty means unset and it must not contain secrets.
	RequestedServiceTier string `json:"requested_service_tier,omitempty"`

	// ObservedServiceTier is the single provider-reported actual service tier when all observed successful requests agree; empty means unreported, unavailable, or mixed and it must not contain secrets.
	ObservedServiceTier string `json:"observed_service_tier,omitempty"`

	// ObservedServiceTierCounts maps provider-reported actual service tier labels to successful request counts; nil means the provider did not report actual tiers and values must not contain secrets.
	ObservedServiceTierCounts map[string]int `json:"observed_service_tier_counts,omitempty"`

	// Model is the provider model identifier for this target; empty means unavailable and it must not contain API keys or credentials.
	Model string `json:"model"`

	// BaseURL is the provider endpoint/base URL with credentials and secret query values redacted; empty means unavailable.
	BaseURL string `json:"base_url,omitempty"`

	// APIKeyEnv is the environment variable name used to resolve the API key; empty means unavailable, the name is not a secret, and the value is never written.
	APIKeyEnv string `json:"api_key_env,omitempty"`

	// IncludeUsage records whether Chat Completions usage chunks were requested; false means disabled or irrelevant for Responses.
	IncludeUsage bool `json:"include_usage"`

	// LegacyMaxTokens records whether Chat Completions legacy max_tokens compatibility was requested; false means the modern field or Responses API was used.
	LegacyMaxTokens bool `json:"legacy_max_tokens"`

	// Extra contains provider-specific JSON-compatible metadata with secrets redacted; nil or empty means no extra target metadata was recorded.
	Extra map[string]any `json:"extra,omitempty"`
}

// WriteOptions configures report file output for one benchmark run.
type WriteOptions struct {
	// OutputDir is the directory that will contain run.json, requests.jsonl, optional chunks.jsonl, summary.json, and summary.md; empty means WriteRun generates a timestamped directory under runs/.
	OutputDir string

	// Overwrite allows replacing an existing non-empty output directory when true; false preserves existing files and returns an error.
	Overwrite bool

	// SaveChunks controls whether chunks.jsonl is written; false omits chunks because they may contain sensitive generated content.
	SaveChunks bool

	// Run is the reproducibility metadata written to run.json; missing runtime fields are filled by WriteRun.
	Run RunMetadata

	// Result is the completed benchmark result to write; nil is invalid.
	Result *whatttft.RunResult
}

// WriteRun writes run metadata, raw request records, optional chunks, JSON summary, and Markdown summary, returning the resolved report directory.
func WriteRun(options WriteOptions) (string, error) {
	if options.Result == nil {
		return "", errors.New("run result is required")
	}

	outputDir := ResolveOutputDir(options.OutputDir, options.Run, time.Now())
	if err := prepareOutputDir(outputDir, options.Overwrite); err != nil {
		return "", err
	}

	metadata := completeRunMetadata(options.Run)
	metadata = applySummaryMetadata(metadata, options.Result.Summary)
	metadata.RunConfig.OutputDir = outputDir
	if err := writeJSON(filepath.Join(outputDir, runJSONName), metadata); err != nil {
		return "", err
	}
	if err := writeJSONL(filepath.Join(outputDir, requestsJSONLName), options.Result.Records); err != nil {
		return "", err
	}
	if options.SaveChunks {
		if err := writeJSONL(filepath.Join(outputDir, chunksJSONLName), options.Result.Chunks); err != nil {
			return "", err
		}
	}
	if err := writeJSON(filepath.Join(outputDir, summaryJSONName), options.Result.Summary); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(outputDir, summaryMarkdownName), []byte(MarkdownSummaryWithMetadata(options.Result.Summary, metadata)), 0o600); err != nil {
		return "", fmt.Errorf("write summary markdown: %w", err)
	}

	return outputDir, nil
}

// ResolveOutputDir returns the explicit outputDir, metadata RunConfig output directory, or a timestamped reports directory under runs/.
func ResolveOutputDir(outputDir string, metadata RunMetadata, at time.Time) string {
	if strings.TrimSpace(outputDir) != "" {
		return outputDir
	}
	if strings.TrimSpace(metadata.RunConfig.OutputDir) != "" {
		return metadata.RunConfig.OutputDir
	}

	return defaultOutputDir(metadata, at)
}

// ValidateOutputDir checks whether outputDir can be used for reports without mutating the filesystem.
func ValidateOutputDir(outputDir string, overwrite bool) error {
	if strings.TrimSpace(outputDir) == "" {
		return nil
	}

	entries, err := os.ReadDir(outputDir)
	if err == nil {
		if len(entries) > 0 && !overwrite {
			return fmt.Errorf("output directory %q is not empty", outputDir)
		}

		return nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	return fmt.Errorf("inspect output directory: %w", err)
}

func defaultOutputDir(metadata RunMetadata, at time.Time) string {
	if at.IsZero() {
		at = time.Now()
	}

	scenario := pathSlug(firstNonEmpty(metadata.Scenario.Name, metadata.RunConfig.Scenario.Name), "scenario")
	cacheMode := pathSlug(string(metadata.RunConfig.CacheMode), "cache")
	connectionMode := pathSlug(string(metadata.RunConfig.ConnectionMode), "connection")
	timestamp := at.UTC().Format("20060102T150405.000000000Z")
	if metadata.BenchmarkName != "" || len(metadata.Targets) > 0 {
		benchmark := pathSlug(metadata.BenchmarkName, "benchmark")
		name := strings.Join([]string{benchmark, scenario, cacheMode, connectionMode, timestamp}, "-")
		return filepath.Join(defaultOutputRoot, name)
	}

	provider := pathSlug(metadata.Provider, "provider")
	model := pathSlug(metadata.Model, "model")
	name := strings.Join([]string{provider, model, scenario, cacheMode, connectionMode, timestamp}, "-")

	return filepath.Join(defaultOutputRoot, name)
}

func pathSlug(value string, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastSeparator := false
	for _, char := range value {
		if slugChar(char) {
			builder.WriteRune(char)
			lastSeparator = false
			continue
		}
		if !lastSeparator {
			builder.WriteByte('-')
			lastSeparator = true
		}
	}

	slug := strings.Trim(builder.String(), "-")
	if len(slug) > 80 {
		slug = strings.Trim(slug[:80], "-")
	}
	if slug == "" {
		return fallback
	}

	return slug
}

func slugChar(char rune) bool {
	return char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-' || char == '_' || char == '.'
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}

// RedactURL removes userinfo and secret-looking query values from rawURL for report metadata.
func RedactURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "[invalid-url]"
	}
	parsed.User = nil
	parsed.Fragment = ""

	query := parsed.Query()
	for key, values := range query {
		if secretQueryKey(key) {
			for index := range values {
				values[index] = "[REDACTED]"
			}
			query[key] = values
		}
	}
	parsed.RawQuery = query.Encode()

	return parsed.String()
}

func completeRunMetadata(metadata RunMetadata) RunMetadata {
	if metadata.BenchmarkVersion == "" {
		metadata.BenchmarkVersion = benchmarkVersion
	}
	if metadata.GitSHA == "" {
		metadata.GitSHA = detectGitSHA()
	}
	if metadata.GoVersion == "" {
		metadata.GoVersion = runtime.Version()
	}
	if metadata.GOOS == "" {
		metadata.GOOS = runtime.GOOS
	}
	if metadata.GOARCH == "" {
		metadata.GOARCH = runtime.GOARCH
	}
	metadata.BaseURL = RedactURL(metadata.BaseURL)
	for index := range metadata.Targets {
		metadata.Targets[index].BaseURL = RedactURL(metadata.Targets[index].BaseURL)
	}

	return metadata
}

func applySummaryMetadata(metadata RunMetadata, summary whatttft.RunSummary) RunMetadata {
	if len(metadata.Targets) == 0 {
		return metadata
	}

	groups := make(map[string]whatttft.SummaryGroup, len(summary.Groups))
	for _, group := range summary.Groups {
		if group.TargetID != "" {
			groups[group.TargetID] = group
		}
	}
	for index := range metadata.Targets {
		group, ok := groups[metadata.Targets[index].TargetID]
		if !ok || len(group.ObservedServiceTierCounts) == 0 {
			continue
		}
		metadata.Targets[index].ObservedServiceTierCounts = copyStringIntMap(group.ObservedServiceTierCounts)
		metadata.Targets[index].ObservedServiceTier = singleMapKey(group.ObservedServiceTierCounts)
	}

	return metadata
}

func copyStringIntMap(values map[string]int) map[string]int {
	if len(values) == 0 {
		return nil
	}

	copyValues := make(map[string]int, len(values))
	for key, value := range values {
		copyValues[key] = value
	}

	return copyValues
}

func singleMapKey(values map[string]int) string {
	if len(values) != 1 {
		return ""
	}
	for key := range values {
		return key
	}

	return ""
}

func prepareOutputDir(outputDir string, overwrite bool) error {
	if err := ValidateOutputDir(outputDir, overwrite); err != nil {
		return err
	}
	if overwrite {
		if err := os.RemoveAll(outputDir); err != nil {
			return fmt.Errorf("remove output directory: %w", err)
		}
	}

	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	return nil
}

func writeJSON(path string, value any) error {
	//nolint:gosec // Report paths are constructed from the caller-selected output directory and fixed filenames.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", filepath.Base(path), err)
	}
	defer closeFile(file)

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("encode %s: %w", filepath.Base(path), err)
	}

	return nil
}

func writeJSONL[T any](path string, values []T) error {
	//nolint:gosec // Report paths are constructed from the caller-selected output directory and fixed filenames.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", filepath.Base(path), err)
	}
	defer closeFile(file)

	writer := bufio.NewWriter(file)
	encoder := json.NewEncoder(writer)
	for _, value := range values {
		if err := encoder.Encode(value); err != nil {
			return fmt.Errorf("encode %s: %w", filepath.Base(path), err)
		}
	}
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("flush %s: %w", filepath.Base(path), err)
	}

	return nil
}

func detectGitSHA() string {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, "git", "rev-parse", "--short", "HEAD")
	output, err := command.Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(output))
}

func secretQueryKey(key string) bool {
	lower := strings.ToLower(key)
	return strings.Contains(lower, "key") || strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") || strings.Contains(lower, "signature")
}

func closeFile(file *os.File) {
	if err := file.Close(); err != nil {
		return
	}
}
