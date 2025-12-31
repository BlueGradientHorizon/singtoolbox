package testers

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"

	// "net/url"
	"sync"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/common/ntp"
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

type SpeedTestSettings struct {
	Mode        SpeedTestMode
	TestURL     string
	Timeout     time.Duration
	TargetBytes int64
}

func NewDownloadTestSettings() SpeedTestSettings {
	return SpeedTestSettings{
		Mode:        Download,
		TestURL:     "https://speed.cloudflare.com/__down",
		Timeout:     20 * time.Second,
		TargetBytes: 10 * 1024 * 1024,
	}
}

func NewUploadTestSettings() SpeedTestSettings {
	return SpeedTestSettings{
		Mode:        Upload,
		TestURL:     "https://speed.cloudflare.com/__up",
		Timeout:     20 * time.Second,
		TargetBytes: 10 * 1024 * 1024,
	}
}

func SpeedTest(
	ctx context.Context,
	sett SpeedTestSettings,
	outbounds []adapter.Outbound,
	outChan chan<- SpeedTestResult,
) ([]SpeedTestResult, error) {
	if sett.TestURL == "" {
		return []SpeedTestResult{}, errors.New("empty link")
	}

	var wg sync.WaitGroup

	resChan := make(chan SpeedTestResult, len(outbounds))

	for _, o := range outbounds {
		wg.Add(1)
		go func(o adapter.Outbound) {
			defer wg.Done()

			testCtx, cancel := context.WithTimeout(ctx, sett.Timeout)
			defer cancel()

			internalChan := make(chan SpeedTestResult, 1)

			go func() {
				speed, err := runSpeedTest(testCtx, sett, o)
				internalChan <- SpeedTestResult{
					Tag:      o.Tag(),
					Speed:    speed,
					Outbound: o,
					Error:    err,
				}
			}()

			select {
			case res := <-internalChan:
				resChan <- res
				if outChan != nil {
					outChan <- res
				}
			case <-testCtx.Done():
				r := SpeedTestResult{
					Tag:      o.Tag(),
					Speed:    -1,
					Outbound: o,
					Error:    testCtx.Err(),
				}
				resChan <- r
				if outChan != nil {
					outChan <- r
				}
			}
		}(o)
	}

	go func() {
		wg.Wait()
		close(resChan)
		if outChan != nil {
			close(outChan)
		}
	}()

	var finalResults []SpeedTestResult
	for res := range resChan {
		finalResults = append(finalResults, res)
	}
	return finalResults, nil
}

func runSpeedTest(ctx context.Context, sett SpeedTestSettings, outbound adapter.Outbound) (float64, error) {
	start := time.Now()
	bytesProcessed, err := performTransfer(ctx, sett.TestURL, outbound, sett.TargetBytes, sett.Mode)
	if err != nil {
		return 0, err
	}

	elapsed := time.Since(start).Seconds()
	if elapsed <= 0 {
		return 0, nil
	}

	return float64(bytesProcessed) / elapsed, nil
}

func performTransfer(ctx context.Context, link string, detour network.Dialer, targetBytes int64, mode SpeedTestMode) (int64, error) {
	var method string
	var body io.Reader
	if mode == Download {
		method = http.MethodGet
		link = fmt.Sprintf("%s?bytes=%d", link, targetBytes)
	} else {
		method = http.MethodPost
		body = io.LimitReader(zeroReader{}, targetBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, link, body)
	if err != nil {
		return -1, err
	}
	if mode == Upload {
		req.ContentLength = targetBytes
	}

	client := http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return detour.DialContext(ctx, network, metadata.ParseSocksaddr(addr))
			},
			DisableKeepAlives: true,
			TLSClientConfig: &tls.Config{
				Time:    ntp.TimeFuncFromContext(ctx),
				RootCAs: adapter.RootPoolFromContext(ctx),
			},
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: constant.TCPTimeout, // equals to  15 * time.Second
	}
	defer client.CloseIdleConnections()

	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		return -1, err
	}
	defer resp.Body.Close()

	if mode == Download {
		return io.CopyN(io.Discard, resp.Body, targetBytes)
	} else {
		return targetBytes, nil
	}
}

type zeroReader struct{}

func (z zeroReader) Read(p []byte) (n int, err error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}
