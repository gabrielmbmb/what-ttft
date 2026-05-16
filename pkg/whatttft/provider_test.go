package whatttft

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestProviderEventJSONShape verifies the stable JSON field names for provider events.
func TestProviderEventJSONShape(t *testing.T) {
	stream := StreamEvent{
		RequestID: "req-001",
		Index:     0,
		Protocol:  "sse",
		AtNS:      100,
		RawBytes:  14,
		DataBytes: 6,
		Empty:     false,
		Terminal:  false,
	}
	output := OutputDelta{
		RequestID: "req-001",
		Index:     0,
		AtNS:      150,
		Text:      "hello",
		Modality:  "text",
		Visible:   true,
	}

	streamData, err := json.Marshal(stream)
	if err != nil {
		t.Fatalf("marshal stream event: %v", err)
	}
	outputData, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("marshal output delta: %v", err)
	}

	if !strings.Contains(string(streamData), `"protocol":"sse"`) {
		t.Fatalf("stream event JSON missing protocol: %s", streamData)
	}
	if !strings.Contains(string(outputData), `"visible":true`) {
		t.Fatalf("output delta JSON missing visibility: %s", outputData)
	}
}
