package testers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sagernet/sing-box/adapter"
)

const (
	SpeedCloudflareDown = "https://speed.cloudflare.com/__down"
	SpeedCloudflareUp   = "https://speed.cloudflare.com/__up"
)

type SpeedTestResult struct {
	Tag      string
	Speed    float64
	Outbound adapter.Outbound
	Error    error
}

type SpeedTestMode int

const (
	Download SpeedTestMode = iota
	Upload
)

type SpeedTestProvider struct {
	GetURL        func(mode SpeedTestMode, targetBytes int64) string
	ModifyRequest func(req *http.Request, mode SpeedTestMode, targetBytes int64)
}

var CloudflareProvider = SpeedTestProvider{
	GetURL: func(mode SpeedTestMode, targetBytes int64) string {
		const (
			Down = "https://speed.cloudflare.com/__down"
			Up   = "https://speed.cloudflare.com/__up"
		)
		var u string
		switch mode {
		case Download:
			u = fmt.Sprintf("%s?bytes=%d", Down, targetBytes)
		case Upload:
			u = Up
		}
		return u
	},
	ModifyRequest: func(req *http.Request, mode SpeedTestMode, targetBytes int64) {
		if mode == Upload {
			req.ContentLength = targetBytes
		}
	},
}

type SpeedTestSettings struct {
	Mode        SpeedTestMode
	Provider    SpeedTestProvider
	Timeout     time.Duration
	TargetBytes int64
}

func NewDownloadTestSettings() SpeedTestSettings {
	return SpeedTestSettings{
		Mode:        Download,
		Provider:    CloudflareProvider,
		Timeout:     20 * time.Second,
		TargetBytes: 10 * 1024 * 1024,
	}
}

func NewUploadTestSettings() SpeedTestSettings {
	return SpeedTestSettings{
		Mode:        Upload,
		Provider:    CloudflareProvider,
		Timeout:     20 * time.Second,
		TargetBytes: 10 * 1024 * 1024,
	}
}

type SpeedTest struct {
	ctx      context.Context
	settings SpeedTestSettings
	items    []speedTestItem
}

type speedTestItem struct {
	outbound adapter.Outbound
	client   *http.Client
}

func NewSpeedTest(ctx context.Context, sett SpeedTestSettings, outbounds []adapter.Outbound) (*SpeedTest, error) {
	if sett.Provider.GetURL == nil {
		return nil, errors.New("NewSpeedTest: provider's GetURL is nil")
	}

	if sett.Provider.ModifyRequest == nil {
		sett.Provider.ModifyRequest = func(r *http.Request, m SpeedTestMode, b int64) {}
	}

	items := make([]speedTestItem, len(outbounds))
	for i, outbound := range outbounds {
		items[i] = speedTestItem{
			outbound: outbound,
			client:   newTestClient(ctx, outbound, nil),
		}
	}

	return &SpeedTest{
		ctx:      ctx,
		settings: sett,
		items:    items,
	}, nil
}

func (t *SpeedTest) Run(resChans ...chan<- SpeedTestResult) {
	runParallel(t.ctx, t.settings.Timeout, len(t.items), func(ctx context.Context, i int) SpeedTestResult {
		item := t.items[i]
		defer item.client.CloseIdleConnections()

		val, err := t.runTest(ctx, item)
		if err != nil {
			val = -1
		}
		return SpeedTestResult{
			Tag:      item.outbound.Tag(),
			Speed:    val,
			Outbound: item.outbound,
			Error:    err,
		}
	}, resChans...)
}

func (t *SpeedTest) runTest(ctx context.Context, item speedTestItem) (float64, error) {
	var method string
	var body io.Reader

	switch t.settings.Mode {
	case Download:
		method = http.MethodGet
	case Upload:
		method = http.MethodPost
		body = io.LimitReader(zeroReader{}, t.settings.TargetBytes)
	}

	finalURL := t.settings.Provider.GetURL(t.settings.Mode, t.settings.TargetBytes)

	req, err := http.NewRequestWithContext(ctx, method, finalURL, body)
	if err != nil {
		return 0, err
	}

	t.settings.Provider.ModifyRequest(req, t.settings.Mode, t.settings.TargetBytes)

	start := time.Now()
	resp, err := item.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var bytesProcessed int64
	if t.settings.Mode == Download {
		bytesProcessed, err = io.CopyN(io.Discard, resp.Body, t.settings.TargetBytes)
		if err != nil {
			return 0, err
		}
	} else {
		bytesProcessed = t.settings.TargetBytes
	}

	elapsed := time.Since(start).Seconds()
	if elapsed <= 0 {
		return 0, nil
	}
	return float64(bytesProcessed) / elapsed, nil
}

type zeroReader struct{}

func (z zeroReader) Read(p []byte) (n int, err error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}
