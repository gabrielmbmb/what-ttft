package httptracecap

import (
	"net/http"
	"time"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

// TransportConfig configures HTTP client transport behavior for benchmark requests.
type TransportConfig struct {
	// ConnectionMode controls whether the transport should reuse HTTP keepalive connections; zero value is treated as warm connections.
	ConnectionMode whatttft.ConnectionMode

	// Timeout is the whole-request client timeout; zero means the http.Client has no overall timeout.
	Timeout time.Duration
}

// NewHTTPClient creates an HTTP client configured for benchmark streaming requests.
func NewHTTPClient(cfg TransportConfig) *http.Client {
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		DisableCompression:    true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 0,
	}

	if cfg.ConnectionMode == whatttft.ColdConnections {
		tr.DisableKeepAlives = true
		tr.MaxIdleConnsPerHost = -1
	}

	return &http.Client{
		Transport: tr,
		Timeout:   cfg.Timeout,
	}
}
