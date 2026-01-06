package testers

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

const (
	Google204         = "https://www.google.com/generate_204"
	GStatic204        = "https://www.gstatic.com/generate_204"
	PlayGoogleAPIs204 = "https://play.googleapis.com/generate_204"
	CPCloudflare204   = "https://cp.cloudflare.com/generate_204"
)

type LatencyTestResult struct {
	Tag      string
	Delay    int32
	Outbound adapter.Outbound
	Error    error
}

type LatencyTestSettings struct {
	TestURL string
	Timeout time.Duration
}

func NewLatencyTestSettings() LatencyTestSettings {
	return LatencyTestSettings{
		TestURL: Google204,
		Timeout: 20 * time.Second,
	}
}

type LatencyTest struct {
	ctx      context.Context
	settings LatencyTestSettings
	items    []latencyTestItem
}

type latencyTestItem struct {
	outbound adapter.Outbound
	client   *http.Client
	start    *time.Time
}

func NewLatencyTest(ctx context.Context, sett LatencyTestSettings, outbounds []adapter.Outbound) (*LatencyTest, error) {
	if sett.TestURL == "" {
		return nil, errors.New("LatencyTest: empty settings link")
	}

	items := make([]latencyTestItem, len(outbounds))
	for i, outbound := range outbounds {
		var startTime time.Time
		dialerMiddleware := func(detour N.Dialer, ctx context.Context, network, addr string) (net.Conn, error) {
			startTime = time.Now()
			instance, err := detour.DialContext(ctx, network, metadata.ParseSocksaddr(addr))
			if err != nil {
				return nil, err
			}
			if earlyConn, isEarlyConn := common.Cast[N.EarlyConn](instance); isEarlyConn && earlyConn.NeedHandshake() {
				startTime = time.Now()
			}
			return instance, nil
		}

		items[i] = latencyTestItem{
			outbound: outbound,
			client:   newTestClient(ctx, outbound, dialerMiddleware),
			start:    &startTime,
		}
	}

	return &LatencyTest{
		ctx:      ctx,
		settings: sett,
		items:    items,
	}, nil
}

func (t *LatencyTest) Run(resChans ...chan<- LatencyTestResult) {
	runParallel(t.ctx, t.settings.Timeout, len(t.items), func(ctx context.Context, i int) LatencyTestResult {
		item := t.items[i]
		defer item.client.CloseIdleConnections()

		val, err := t.runTest(ctx, item)
		if err != nil {
			val = -1
		}
		return LatencyTestResult{
			Tag:      item.outbound.Tag(),
			Delay:    int32(val),
			Outbound: item.outbound,
			Error:    err,
		}
	}, resChans...)
}

func (t *LatencyTest) runTest(ctx context.Context, item latencyTestItem) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, t.settings.TestURL, nil)
	if err != nil {
		return -1, err
	}

	resp, err := item.client.Do(req)
	if err != nil {
		return -1, err
	}
	defer resp.Body.Close()

	return int64(time.Since(*item.start) / time.Millisecond), nil
}
