// Package sse parses Server-Sent Events streams without provider-specific logic.
package sse

import (
	"bufio"
	"bytes"
	"errors"
	"io"
)

// Event is one parsed Server-Sent Events event block.
type Event struct {
	// Data is the joined SSE data payload with multi-line data fields separated by '\n'; nil means no data field was present and empty means at least one empty data field was present.
	Data []byte

	// Event is the optional SSE event type from an event field; empty means no event type was present.
	Event string

	// ID is the optional SSE event ID from an id field; empty means no ID was present.
	ID string

	// Retry is the optional SSE retry value from a retry field; empty means no retry value was present.
	Retry string

	// RawBytes is the byte count read for this event block, including line endings and ignored comments within the block; zero means no bytes were recorded.
	RawBytes int
}

// Parser incrementally reads Server-Sent Events from an io.Reader.
type Parser struct {
	r *bufio.Reader
}

// New creates a Parser that reads Server-Sent Events from r.
func New(r io.Reader) *Parser {
	return &Parser{r: bufio.NewReaderSize(r, 64*1024)}
}

// Next returns the next SSE event containing at least one data field.
func (p *Parser) Next() (Event, error) {
	var event eventBuilder

	for {
		line, err := p.r.ReadBytes('\n')
		if len(line) > 0 {
			dispatched, parsed := event.addLine(line)
			if dispatched {
				return parsed, nil
			}
		}

		if err != nil {
			if errors.Is(err, io.EOF) && event.hasData {
				return event.build(), nil
			}

			return Event{}, err
		}
	}
}

type eventBuilder struct {
	data      bytes.Buffer
	dataLines int
	event     string
	id        string
	retry     string
	rawBytes  int
	hasData   bool
}

func (b *eventBuilder) addLine(line []byte) (bool, Event) {
	b.rawBytes += len(line)

	trimmed := bytes.TrimRight(line, "\r\n")
	if len(trimmed) == 0 {
		if b.hasData {
			return true, b.build()
		}

		b.reset()
		return false, Event{}
	}

	if trimmed[0] == ':' {
		return false, Event{}
	}

	field, value := splitField(trimmed)
	switch string(field) {
	case "data":
		b.hasData = true
		if b.dataLines > 0 {
			b.data.WriteByte('\n')
		}
		b.data.Write(value)
		b.dataLines++
	case "event":
		b.event = string(value)
	case "id":
		b.id = string(value)
	case "retry":
		b.retry = string(value)
	}

	return false, Event{}
}

func (b *eventBuilder) build() Event {
	data := make([]byte, b.data.Len())
	copy(data, b.data.Bytes())

	return Event{
		Data:     data,
		Event:    b.event,
		ID:       b.id,
		Retry:    b.retry,
		RawBytes: b.rawBytes,
	}
}

func (b *eventBuilder) reset() {
	b.data.Reset()
	b.dataLines = 0
	b.event = ""
	b.id = ""
	b.retry = ""
	b.rawBytes = 0
	b.hasData = false
}

func splitField(line []byte) ([]byte, []byte) {
	field, value, ok := bytes.Cut(line, []byte{':'})
	if !ok {
		return field, nil
	}

	if len(value) > 0 && value[0] == ' ' {
		value = value[1:]
	}

	return field, value
}
