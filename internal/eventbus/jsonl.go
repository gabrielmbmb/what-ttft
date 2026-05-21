package eventbus

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

// JSONLSink writes live benchmark events as one JSON object per line.
type JSONLSink struct {
	mu     sync.Mutex
	writer *bufio.Writer
	closer io.Closer
	closed bool
}

// NewJSONLSink creates a JSONL event sink at path, replacing any existing file.
func NewJSONLSink(path string) (*JSONLSink, error) {
	if path == "" {
		return nil, errors.New("event JSONL path is required")
	}
	parent := filepath.Dir(path)
	if parent != "." && parent != "" {
		if err := os.MkdirAll(parent, 0o700); err != nil {
			return nil, fmt.Errorf("create event JSONL parent directory: %w", err)
		}
	}

	//nolint:gosec // Event JSONL paths are explicit CLI/user-provided output paths by design.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open event JSONL: %w", err)
	}

	return newJSONLSinkWriter(file, file), nil
}

func newJSONLSinkWriter(writer io.Writer, closer io.Closer) *JSONLSink {
	return &JSONLSink{writer: bufio.NewWriter(writer), closer: closer}
}

// Publish writes event as one JSON object followed by a newline and flushes it for live tailing.
func (s *JSONLSink) Publish(ctx context.Context, event whatttft.RunEvent) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("event JSONL sink is closed")
	}

	encoder := json.NewEncoder(s.writer)
	if err := encoder.Encode(event.Clone()); err != nil {
		return fmt.Errorf("write event JSONL: %w", err)
	}
	if err := s.writer.Flush(); err != nil {
		return fmt.Errorf("flush event JSONL: %w", err)
	}

	return nil
}

// Close flushes buffered event JSONL data and closes the underlying file when one exists.
func (s *JSONLSink) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true

	flushErr := s.writer.Flush()
	var closeErr error
	if s.closer != nil {
		closeErr = s.closer.Close()
	}

	return errors.Join(flushErr, closeErr)
}
