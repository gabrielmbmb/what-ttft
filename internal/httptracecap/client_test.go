package httptracecap

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

// TestNewHTTPClientConfiguresBenchmarkTransport verifies the default transport avoids hidden streaming changes.
func TestNewHTTPClientConfiguresBenchmarkTransport(t *testing.T) {
	client := NewHTTPClient(TransportConfig{Timeout: 12 * time.Second})

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", client.Transport)
	}
	if client.Timeout != 12*time.Second {
		t.Fatalf("timeout = %s, want 12s", client.Timeout)
	}
	if !transport.ForceAttemptHTTP2 {
		t.Fatal("ForceAttemptHTTP2 should be enabled")
	}
	if !transport.DisableCompression {
		t.Fatal("DisableCompression should be enabled")
	}
	if transport.MaxIdleConns != 100 {
		t.Fatalf("MaxIdleConns = %d, want 100", transport.MaxIdleConns)
	}
	if transport.MaxIdleConnsPerHost != 100 {
		t.Fatalf("MaxIdleConnsPerHost = %d, want 100", transport.MaxIdleConnsPerHost)
	}
	if transport.IdleConnTimeout != 90*time.Second {
		t.Fatalf("IdleConnTimeout = %s, want 1m30s", transport.IdleConnTimeout)
	}
	if transport.TLSHandshakeTimeout != 10*time.Second {
		t.Fatalf("TLSHandshakeTimeout = %s, want 10s", transport.TLSHandshakeTimeout)
	}
}

// TestNewHTTPClientWarmModeReusesConnection verifies warm mode allows keepalive reuse.
func TestNewHTTPClientWarmModeReusesConnection(t *testing.T) {
	server := newKeepaliveServer(t)
	defer server.Close()

	client := NewHTTPClient(TransportConfig{
		ConnectionMode: whatttft.WarmConnections,
		Timeout:        5 * time.Second,
	})

	first := performClientTraceRequest(t, client, server.URL)
	second := performClientTraceRequest(t, client, server.URL)

	if first.ConnReused {
		t.Fatal("first request unexpectedly reused a connection")
	}
	if !second.ConnReused {
		t.Fatal("second warm request should reuse a connection")
	}
}

// TestNewHTTPClientColdModeDoesNotReuseConnection verifies cold mode disables HTTP keepalive reuse.
func TestNewHTTPClientColdModeDoesNotReuseConnection(t *testing.T) {
	server := newKeepaliveServer(t)
	defer server.Close()

	client := NewHTTPClient(TransportConfig{
		ConnectionMode: whatttft.ColdConnections,
		Timeout:        5 * time.Second,
	})

	first := performClientTraceRequest(t, client, server.URL)
	second := performClientTraceRequest(t, client, server.URL)

	if first.ConnReused {
		t.Fatal("first request unexpectedly reused a connection")
	}
	if second.ConnReused {
		t.Fatal("second cold request should not reuse a connection")
	}
}

// TestNewHTTPClientColdModeConfiguresTransport verifies cold mode sets transport keepalive controls.
func TestNewHTTPClientColdModeConfiguresTransport(t *testing.T) {
	client := NewHTTPClient(TransportConfig{ConnectionMode: whatttft.ColdConnections})

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", client.Transport)
	}
	if !transport.DisableKeepAlives {
		t.Fatal("DisableKeepAlives should be enabled in cold mode")
	}
	if transport.MaxIdleConnsPerHost != -1 {
		t.Fatalf("MaxIdleConnsPerHost = %d, want -1", transport.MaxIdleConnsPerHost)
	}
}

func newKeepaliveServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		if _, err := w.Write([]byte("ok")); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
}

func performClientTraceRequest(t *testing.T, client *http.Client, url string) whatttft.HTTPRecord {
	t.Helper()

	rec := whatttft.NewRecorder(nil)
	capture := NewCapture(CaptureConfig{CompressionDisabled: true})

	resp := doTracedRequest(t, client, url, rec, capture)
	defer closeBody(t, resp)

	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatalf("read response body: %v", err)
	}

	return capture.Record()
}
