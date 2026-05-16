// Package httptracecap captures net/http/httptrace events into whatttft records.
package httptracecap

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptrace"
	"sync"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

// CaptureConfig configures HTTP trace capture metadata that cannot be observed from httptrace callbacks alone.
type CaptureConfig struct {
	// CompressionDisabled records whether the HTTP transport disabled automatic response compression; false means compression may be enabled or unknown.
	CompressionDisabled bool
}

// Capture stores HTTP trace metadata captured for one request.
type Capture struct {
	mu     sync.Mutex
	record whatttft.HTTPRecord
}

// NewCapture creates an HTTP trace capture initialized with static transport metadata from cfg.
func NewCapture(cfg CaptureConfig) *Capture {
	return &Capture{
		record: whatttft.HTTPRecord{
			CompressionDisabled: cfg.CompressionDisabled,
		},
	}
}

// WithTrace attaches HTTP trace callbacks for capture to ctx.
func WithTrace(ctx context.Context, rec *whatttft.Recorder, capture *Capture) context.Context {
	if capture == nil {
		capture = NewCapture(CaptureConfig{})
	}

	return capture.WithTrace(ctx, rec)
}

// WithTrace attaches HTTP trace callbacks for c to ctx.
func (c *Capture) WithTrace(ctx context.Context, rec *whatttft.Recorder) context.Context {
	trace := &httptrace.ClientTrace{
		DNSStart: func(_ httptrace.DNSStartInfo) {
			mark(rec, whatttft.EventDNSStart)
		},
		DNSDone: func(info httptrace.DNSDoneInfo) {
			mark(rec, whatttft.EventDNSDone)
			c.update(func(record *whatttft.HTTPRecord) {
				record.DNSAddrs = len(info.Addrs)
				if info.Err != nil {
					record.DNSError = info.Err.Error()
				}
			})
		},
		ConnectStart: func(network string, addr string) {
			mark(rec, whatttft.EventConnectStart)
			c.update(func(record *whatttft.HTTPRecord) {
				record.Network = network
				record.RemoteAddr = addr
			})
		},
		ConnectDone: func(_ string, _ string, err error) {
			mark(rec, whatttft.EventConnectDone)
			if err != nil {
				c.update(func(record *whatttft.HTTPRecord) {
					record.ConnectError = err.Error()
				})
			}
		},
		TLSHandshakeStart: func() {
			mark(rec, whatttft.EventTLSStart)
		},
		TLSHandshakeDone: func(state tls.ConnectionState, err error) {
			mark(rec, whatttft.EventTLSDone)
			c.update(func(record *whatttft.HTTPRecord) {
				record.TLSVersion = tlsVersionString(state.Version)
				if err != nil {
					record.TLSError = err.Error()
				}
			})
		},
		GotConn: func(info httptrace.GotConnInfo) {
			mark(rec, whatttft.EventGotConn)
			c.update(func(record *whatttft.HTTPRecord) {
				record.GotConn = true
				record.ConnReused = info.Reused
				record.ConnWasIdle = info.WasIdle
				record.ConnIdleTimeNS = info.IdleTime.Nanoseconds()
			})
		},
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			mark(rec, whatttft.EventWroteRequest)
			if info.Err != nil {
				c.update(func(record *whatttft.HTTPRecord) {
					record.WriteError = info.Err.Error()
				})
			}
		},
		GotFirstResponseByte: func() {
			if rec != nil {
				rec.MarkFirst(whatttft.EventFirstResponseByte)
			}
		},
	}

	return httptrace.WithClientTrace(ctx, trace)
}

// ObserveResponse records response status and protocol metadata after client.Do returns.
func (c *Capture) ObserveResponse(resp *http.Response) {
	if resp == nil {
		return
	}

	c.update(func(record *whatttft.HTTPRecord) {
		record.StatusCode = resp.StatusCode
		record.Status = resp.Status
		record.Protocol = resp.Proto
	})
}

// Record returns a concurrency-safe snapshot of the captured HTTP metadata.
func (c *Capture) Record() whatttft.HTTPRecord {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.record
}

func (c *Capture) update(update func(*whatttft.HTTPRecord)) {
	c.mu.Lock()
	defer c.mu.Unlock()

	update(&c.record)
}

func mark(rec *whatttft.Recorder, name whatttft.EventName) {
	if rec != nil {
		rec.Mark(name)
	}
}

func tlsVersionString(version uint16) string {
	switch version {
	case 0:
		return ""
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return fmt.Sprintf("0x%04x", version)
	}
}
