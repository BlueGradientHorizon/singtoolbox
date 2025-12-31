package testers

import (
	"context"
	"sync"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/urltest"
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
		TestURL: "https://www.google.com/generate_204", //"http://cp.cloudflare.com/generate_204",
		Timeout: 20 * time.Second,
	}
}

func LatencyTest(
	ctx context.Context,
	sett LatencyTestSettings,
	outbounds []adapter.Outbound,
	outChan chan<- LatencyTestResult,
) []LatencyTestResult {
	var wg sync.WaitGroup

	resChan := make(chan LatencyTestResult, len(outbounds))

	for _, o := range outbounds {
		wg.Add(1)
		go func(tag string) {
			defer wg.Done()

			testCtx, cancel := context.WithTimeout(context.Background(), sett.Timeout)
			defer cancel()

			internalChan := make(chan LatencyTestResult, 1)

			go func() {
				t, err := urltest.URLTest(testCtx, sett.TestURL, o)
				internalChan <- LatencyTestResult{
					Tag:      o.Tag(),
					Delay:    int32(t),
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
				r := LatencyTestResult{
					Tag:      o.Tag(),
					Delay:    -1,
					Outbound: o,
					Error:    testCtx.Err(),
				}
				resChan <- r
				if outChan != nil {
					outChan <- r
				}
			}
		}(o.Tag())
	}

	go func() {
		wg.Wait()
		close(resChan)
		if outChan != nil {
			close(outChan)
		}
	}()

	var finalResults []LatencyTestResult

	for res := range resChan {
		finalResults = append(finalResults, res)
	}

	return finalResults
}
