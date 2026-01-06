package testers

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/common/ntp"
)

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

func newTestClient(ctx context.Context, detour N.Dialer, dialerMiddleware func(N.Dialer, context.Context, string, string) (net.Conn, error)) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DisableKeepAlives: true,
			TLSClientConfig: &tls.Config{
				Time:    ntp.TimeFuncFromContext(ctx),
				RootCAs: adapter.RootPoolFromContext(ctx),
			},
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				if dialerMiddleware != nil {
					return dialerMiddleware(detour, ctx, network, addr)
				}
				return detour.DialContext(ctx, network, metadata.ParseSocksaddr(addr))
			},
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
