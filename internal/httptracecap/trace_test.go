package httptracecap

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

// TestCaptureRecordsFirstResponseByteAndConnection verifies httptrace lifecycle events are recorded.
func TestCaptureRecordsFirstResponseByteAndConnection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("ok")); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	rec := whatttft.NewRecorder(nil)
	capture := NewCapture(CaptureConfig{CompressionDisabled: true})
	client := NewHTTPClient(TransportConfig{Timeout: 5 * time.Second})

	resp := doTracedRequest(t, client, server.URL, rec, capture)
	defer closeBody(t, resp)

	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatalf("read response body: %v", err)
	}
	rec.Mark(whatttft.EventBodyEOF)

	timeline := rec.Timeline()
	if _, ok := timeline.EventsNS[whatttft.EventGotConn]; !ok {
		t.Fatal("got_conn event was not recorded")
	}
	if _, ok := timeline.EventsNS[whatttft.EventWroteRequest]; !ok {
		t.Fatal("wrote_request event was not recorded")
	}
	if _, ok := timeline.EventsNS[whatttft.EventFirstResponseByte]; !ok {
		t.Fatal("first_response_byte event was not recorded")
	}

	httpRecord := capture.Record()
	if !httpRecord.GotConn {
		t.Fatal("HTTP record did not capture GotConn")
	}
	if httpRecord.StatusCode != http.StatusOK {
		t.Fatalf("status code = %d, want %d", httpRecord.StatusCode, http.StatusOK)
	}
	if httpRecord.Protocol == "" {
		t.Fatal("protocol should be captured")
	}
	if !httpRecord.CompressionDisabled {
		t.Fatal("compression-disabled metadata should be preserved")
	}
}

// TestCaptureRecordsTLSVersion verifies TLS handshake metadata is normalized.
func TestCaptureRecordsTLSVersion(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte("secure")); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	rec := whatttft.NewRecorder(nil)
	capture := NewCapture(CaptureConfig{CompressionDisabled: false})
	client := server.Client()

	resp := doTracedRequest(t, client, server.URL, rec, capture)
	defer closeBody(t, resp)

	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatalf("read response body: %v", err)
	}

	timeline := rec.Timeline()
	if _, ok := timeline.EventsNS[whatttft.EventTLSStart]; !ok {
		t.Fatal("tls_start event was not recorded")
	}
	if _, ok := timeline.EventsNS[whatttft.EventTLSDone]; !ok {
		t.Fatal("tls_done event was not recorded")
	}

	httpRecord := capture.Record()
	if httpRecord.TLSVersion == "" {
		t.Fatal("TLS version should be captured")
	}
}

// TestCaptureRecordReturnsCopy verifies Record snapshots cannot mutate capture state.
func TestCaptureRecordReturnsCopy(t *testing.T) {
	capture := NewCapture(CaptureConfig{})
	capture.ObserveResponse(&http.Response{StatusCode: http.StatusAccepted, Status: "202 Accepted", Proto: "HTTP/1.1"})
	capture.ObserveProviderProcessingMS(42)

	snapshot := capture.Record()
	snapshot.StatusCode = http.StatusTeapot
	if snapshot.ProviderProcessingMS == nil {
		t.Fatal("snapshot provider processing ms should be populated")
	}
	*snapshot.ProviderProcessingMS = 99

	fresh := capture.Record()
	if fresh.StatusCode != http.StatusAccepted {
		t.Fatalf("fresh status = %d, want %d", fresh.StatusCode, http.StatusAccepted)
	}
	if fresh.ProviderProcessingMS == nil || *fresh.ProviderProcessingMS != 42 {
		t.Fatalf("fresh provider processing ms = %v, want 42", fresh.ProviderProcessingMS)
	}
}

// TestTLSVersionString verifies known TLS versions use stable labels.
func TestTLSVersionString(t *testing.T) {
	tests := map[uint16]string{
		0:                "",
		tls.VersionTLS10: "TLS 1.0",
		tls.VersionTLS11: "TLS 1.1",
		tls.VersionTLS12: "TLS 1.2",
		tls.VersionTLS13: "TLS 1.3",
		0xffff:           "0xffff",
	}

	for version, want := range tests {
		got := tlsVersionString(version)
		if got != want {
			t.Fatalf("tlsVersionString(%#x) = %q, want %q", version, got, want)
		}
	}
}

func doTracedRequest(
	t *testing.T,
	client *http.Client,
	url string,
	rec *whatttft.Recorder,
	capture *Capture,
) *http.Response {
	t.Helper()

	rec.Mark(whatttft.EventRequestStart)
	ctx := WithTrace(context.Background(), rec, capture)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	//nolint:gosec // Tests pass only httptest server URLs or fixed local TLS server URLs to this helper.
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("perform request: %v", err)
	}
	rec.Mark(whatttft.EventHeadersReceived)
	capture.ObserveResponse(resp)

	return resp
}

func closeBody(t *testing.T, resp *http.Response) {
	t.Helper()

	if err := resp.Body.Close(); err != nil {
		t.Errorf("close response body: %v", err)
	}
}
