package testers

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

// DialerFunc is a generic function type for establishing connections.
// It abstracts away the specific proxy core implementation details.
type DialerFunc func(ctx context.Context, network, address string) (net.Conn, error)

// TLSConfigProvider is a function that provides TLS configuration for HTTP clients.
// This allows different proxy cores to inject their own TLS settings.
type TLSConfigProvider func(ctx context.Context) *tls.Config

// runParallel executes a test function in parallel across multiple goroutines.
// Results are sent to all provided result channels.
func runParallel[R any](
	ctx context.Context,
	timeout time.Duration,
	count int,
	testFunc func(context.Context, int) R,
	resChans ...chan<- R,
) {
	for i := range count {
		go func(index int) {
			testCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			res := testFunc(testCtx, index)

			for _, c := range resChans {
				if c != nil {
					c <- res
				}
			}
		}(i)
	}
}

// newTestClient creates an HTTP client with a custom dialer and optional TLS configuration.
// This is the core-agnostic version that works with any proxy implementation.
func newTestClient(ctx context.Context, dialer DialerFunc, tlsConfigProvider TLSConfigProvider) *http.Client {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: false,
	}

	// Allow proxy cores to provide custom TLS configuration
	if tlsConfigProvider != nil {
		if customTLS := tlsConfigProvider(ctx); customTLS != nil {
			tlsConfig = customTLS
		}
	}

	return &http.Client{
		Transport: &http.Transport{
			DisableKeepAlives: true,
			TLSClientConfig:   tlsConfig,
			DialContext:       dialer,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
