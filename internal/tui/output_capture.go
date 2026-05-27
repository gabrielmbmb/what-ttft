package tui

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"

	tea "charm.land/bubbletea/v2"
)

const (
	chunksJSONLFileName           = "chunks.jsonl"
	outputCaptureMaxBytes         = 8 * 1024
	outputCaptureTruncationMarker = "\n[output truncated in TUI; inspect chunks.jsonl for full captured content]"
)

type outputCaptureStatus string

const (
	outputCaptureStatusDisabled outputCaptureStatus = "disabled"
	outputCaptureStatusPending  outputCaptureStatus = "pending"
	outputCaptureStatusLoading  outputCaptureStatus = "loading"
	outputCaptureStatusLoaded   outputCaptureStatus = "loaded"
	outputCaptureStatusFailed   outputCaptureStatus = "failed"
)

type outputCapture struct {
	Content       string
	VisibleChunks int
	OriginalBytes int
	RetainedBytes int
	Truncated     bool
}

type outputCaptureLoadedMsg struct {
	OutputDir string
	Captures  map[string]outputCapture
	Err       error
}

func loadOutputCapturesCmd(outputDir string) tea.Cmd {
	return func() tea.Msg {
		captures, err := loadOutputCaptures(outputDir)
		return outputCaptureLoadedMsg{OutputDir: outputDir, Captures: captures, Err: err}
	}
}

func loadOutputCaptures(outputDir string) (map[string]outputCapture, error) {
	captures := make(map[string]outputCapture)
	if strings.TrimSpace(outputDir) == "" {
		return captures, errors.New("output directory unavailable")
	}

	//nolint:gosec // The TUI reads the caller-selected report directory and a fixed report filename.
	file, err := os.Open(filepath.Join(outputDir, chunksJSONLFileName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return captures, nil
		}
		return nil, fmt.Errorf("open chunks.jsonl: %s", outputLoadErrorString(err))
	}
	defer closeOutputCaptureFile(file)

	chunksByRequest := make(map[string][]whatttft.ChunkRecord)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var chunk whatttft.ChunkRecord
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			return nil, fmt.Errorf("decode chunks.jsonl line %d: %w", lineNumber, err)
		}
		if chunk.RequestID == "" {
			continue
		}
		chunksByRequest[chunk.RequestID] = append(chunksByRequest[chunk.RequestID], chunk)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read chunks.jsonl: %s", outputLoadErrorString(err))
	}

	for requestID, chunks := range chunksByRequest {
		sort.SliceStable(chunks, func(i int, j int) bool {
			if chunks[i].Index != chunks[j].Index {
				return chunks[i].Index < chunks[j].Index
			}
			return chunks[i].AtNS < chunks[j].AtNS
		})
		captures[requestID] = buildOutputCapture(chunks)
	}

	return captures, nil
}

func buildOutputCapture(chunks []whatttft.ChunkRecord) outputCapture {
	capture := outputCapture{}
	for _, chunk := range chunks {
		if chunk.UsageChunk || chunk.Content == "" {
			continue
		}
		appendOutputCaptureContent(&capture, chunk.Content)
	}
	if capture.Truncated && !strings.HasSuffix(capture.Content, outputCaptureTruncationMarker) {
		capture.Content += outputCaptureTruncationMarker
		capture.RetainedBytes = len(capture.Content)
	}
	return capture
}

func appendOutputCaptureContent(capture *outputCapture, content string) {
	capture.VisibleChunks++
	capture.OriginalBytes += len(content)
	if capture.Truncated {
		return
	}
	remaining := outputCaptureMaxBytes - len(capture.Content)
	if remaining <= 0 {
		capture.Truncated = true
		return
	}
	if len(content) <= remaining {
		capture.Content += content
		capture.RetainedBytes = len(capture.Content)
		return
	}
	capture.Content += prefixWithinBytes(content, remaining)
	capture.RetainedBytes = len(capture.Content)
	capture.Truncated = true
}

func prefixWithinBytes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	cut := 0
	used := 0
	for _, char := range value {
		width := utf8.RuneLen(char)
		if width < 0 {
			width = len(string(char))
		}
		if used+width > limit {
			break
		}
		used += width
		cut += width
	}
	return value[:cut]
}

func (s *liveStore) configureOutputCapture(saveChunks bool) {
	s.saveChunks = saveChunks
	if saveChunks {
		if s.outputCaptureStatus == "" || s.outputCaptureStatus == outputCaptureStatusDisabled {
			s.outputCaptureStatus = outputCaptureStatusPending
		}
		return
	}
	s.outputCaptureStatus = outputCaptureStatusDisabled
	s.outputCaptureError = ""
	s.outputCaptures = nil
}

func (s *liveStore) startOutputCaptureLoad() {
	if !s.saveChunks {
		return
	}
	s.outputCaptureStatus = outputCaptureStatusLoading
	s.outputCaptureError = ""
}

func (s *liveStore) applyOutputCaptureLoaded(msg outputCaptureLoadedMsg) {
	if !s.saveChunks {
		return
	}
	if msg.OutputDir != "" && s.outputDir != "" && msg.OutputDir != s.outputDir {
		return
	}
	if msg.Err != nil {
		s.outputCaptureStatus = outputCaptureStatusFailed
		s.outputCaptureError = requestDetailRedacted(msg.Err.Error())
		s.reportStatus = "output load failed"
		return
	}
	s.outputCaptureStatus = outputCaptureStatusLoaded
	s.outputCaptureError = ""
	s.outputCaptures = cloneOutputCaptures(msg.Captures)
}

func (s liveStore) outputCaptureFor(requestID string) (outputCapture, bool) {
	capture, ok := s.outputCaptures[requestID]
	return capture, ok
}

func cloneOutputCaptures(captures map[string]outputCapture) map[string]outputCapture {
	if len(captures) == 0 {
		return make(map[string]outputCapture)
	}
	cloned := make(map[string]outputCapture, len(captures))
	for requestID, capture := range captures {
		cloned[requestID] = capture
	}
	return cloned
}

func outputLoadErrorString(err error) string {
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return requestDetailRedacted(pathErr.Op + ": " + pathErr.Err.Error())
	}
	return requestDetailRedacted(err.Error())
}

func closeOutputCaptureFile(file *os.File) {
	if err := file.Close(); err != nil {
		return
	}
}
