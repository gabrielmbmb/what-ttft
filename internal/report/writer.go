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
	benchmarkVersion    = "v0.1-dev"
	chunksJSONLName     = "chunks.jsonl"
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

	// Provider is the normalized provider name for this run; empty means unspecified and it must not contain secrets.
	Provider string `json:"provider"`

	// Model is the provider model identifier for this run; empty means unspecified and it must not contain API keys or credentials.
	Model string `json:"model"`

	// BaseURL is the provider endpoint/base URL with credentials and secret query values redacted; empty means unavailable.
	BaseURL string `json:"base_url,omitempty"`

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

// WriteOptions configures report file output for one benchmark run.
type WriteOptions struct {
	// OutputDir is the directory that will contain run.json, requests.jsonl, optional chunks.jsonl, summary.json, and summary.md; empty is invalid.
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

// WriteRun writes run metadata, raw request records, optional chunks, JSON summary, and Markdown summary to OutputDir.
func WriteRun(options WriteOptions) error {
	if options.OutputDir == "" {
		return errors.New("output directory is required")
	}
	if options.Result == nil {
		return errors.New("run result is required")
	}

	if err := prepareOutputDir(options.OutputDir, options.Overwrite); err != nil {
		return err
	}

	metadata := completeRunMetadata(options.Run)
	if err := writeJSON(filepath.Join(options.OutputDir, runJSONName), metadata); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(options.OutputDir, requestsJSONLName), options.Result.Records); err != nil {
		return err
	}
	if options.SaveChunks {
		if err := writeJSONL(filepath.Join(options.OutputDir, chunksJSONLName), options.Result.Chunks); err != nil {
			return err
		}
	}
	if err := writeJSON(filepath.Join(options.OutputDir, summaryJSONName), options.Result.Summary); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(options.OutputDir, summaryMarkdownName), []byte(MarkdownSummary(options.Result.Summary)), 0o600); err != nil {
		return fmt.Errorf("write summary markdown: %w", err)
	}

	return nil
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

	return metadata
}

func prepareOutputDir(outputDir string, overwrite bool) error {
	entries, err := os.ReadDir(outputDir)
	if err == nil {
		if len(entries) > 0 && !overwrite {
			return fmt.Errorf("output directory %q is not empty", outputDir)
		}
		if overwrite {
			if removeErr := os.RemoveAll(outputDir); removeErr != nil {
				return fmt.Errorf("remove output directory: %w", removeErr)
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect output directory: %w", err)
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
