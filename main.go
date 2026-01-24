package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/bluegradienthorizon/singtoolbox/adapters/singbox"
	"github.com/bluegradienthorizon/singtoolbox/core"
	"github.com/bluegradienthorizon/singtoolbox/parsers"
	"github.com/bluegradienthorizon/singtoolbox/printers"
	"github.com/bluegradienthorizon/singtoolbox/testers"
	"github.com/bluegradienthorizon/singtoolbox/tools"
	"github.com/bluegradienthorizon/singtoolbox/utils"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
)

func main() {
	inputFile := "link_list.txt"
	outputFile := "configs.txt"
	tools.DownloadConfigs(inputFile, outputFile, 10*time.Second)

	fmt.Printf("Attempting to load configurations from file: %s\n", outputFile)

	var profiles []parsers.ProxyProfile
	data, err := os.ReadFile(outputFile)
	if err != nil {
		fmt.Printf("File %s not found\n", outputFile)
		return
	}

	var profilesConnUris []string

	content := strings.TrimSpace(string(data))
	for _, line := range strings.Split(content, "\n") {
		profilesConnUris = append(profilesConnUris, line)
	}

	fmt.Println("before dedup:", len(profilesConnUris))
	profilesConnUris = utils.DeduplicateConnUris(profilesConnUris)
	fmt.Println("after dedup:", len(profilesConnUris))

	parsingErrorsMap := make(map[string]int)

	for _, connUri := range profilesConnUris {
		p, err := parsers.ParseProfile(connUri)

		if err != nil {
			parsingErrorsMap[err.Error()]++
			continue
		}

		profiles = append(profiles, *p)
	}

	println("parsing errors:")

	for err, count := range parsingErrorsMap {
		fmt.Println(count, "x", err)
	}

	if len(profiles) == 0 {
		fmt.Println("! No valid configurations were loaded. Check your source or subscription content.")
		return
	}

	// Create sing-box adapter for converting generic configs
	sbAdapter := singbox.NewAdapter()

	validationErrorsMap := make(map[string]int)

	i := 0
	for _, p := range profiles {
		// Convert generic config to sing-box outbound
		sbOutbound, err := sbAdapter.ConvertOutbound(p.Config)
		if err != nil {
			validationErrorsMap[p.Config.Type+": "+err.Error()]++
			continue
		}

		ctx := include.Context(context.Background())
		instance, err := box.New(box.Options{
			Context: ctx,
			Options: option.Options{
				Outbounds: []option.Outbound{*sbOutbound},
			},
		})
		if err != nil {
			validationErrorsMap[p.Config.Type+": "+err.Error()]++
			continue
		}
		instance.Close()
		profiles[i] = p
		i++
	}
	profiles = profiles[:i]

	println("validation errors:")

	for err, count := range validationErrorsMap {
		fmt.Println(count, "x", err)
	}

	for i := range profiles {
		profiles[i].Config.Tag = fmt.Sprintf("outbound-%d", i)
	}

	ctx := include.Context(context.Background())

	var outbounds []option.Outbound
	for _, p := range profiles {
		// Convert generic config to sing-box outbound
		sbOutbound, err := sbAdapter.ConvertOutbound(p.Config)
		if err != nil {
			fmt.Printf("Failed to convert config: %v\n", err)
			continue
		}
		outbounds = append(outbounds, *sbOutbound)
	}

	opts := option.Options{
		Log: &option.LogOptions{
			Level:     "panic",
			Timestamp: true,
		},
		Outbounds: outbounds,
	}

	instance, err := box.New(box.Options{
		Context: ctx,
		Options: opts,
	})

	if err != nil {
		fmt.Printf("Create sing-box failed: %v\n", err)
		return
	}

	err = instance.Start()
	if err != nil {
		fmt.Printf("Start sing-box failed: %v\n", err)
		return
	}

	fmt.Println("sing-box started successfully.")

	var results []testers.LatencyTestResult

	latencyTestCtx, latencyTestCtxCancel := context.WithCancel(ctx)
	defer latencyTestCtxCancel()

	// go func() {
	// 	time.Sleep(2 * time.Second)
	// 	latencyTestCtxCancel()
	// }()

	rounds := 3

	for i := range rounds {
		if latencyTestCtx.Err() != nil {
			println("test ended prematurely: " + latencyTestCtx.Err().Error())
			break
		}
		var wrappedOutbounds []core.Outbound
		if i == 0 {
			// Wrap sing-box outbounds for first round
			sbOutbounds := instance.Outbound().Outbounds()
			for _, sbOut := range sbOutbounds {
				wrappedOutbounds = append(wrappedOutbounds, singbox.NewOutboundWrapper(sbOut))
			}
		} else {
			// Rebuild outbounds from previous results by tag
			for _, r := range results {
				// Find the outbound by tag
				sbOutbounds := instance.Outbound().Outbounds()
				for _, sbOut := range sbOutbounds {
					if sbOut.Tag() == r.Tag {
						wrappedOutbounds = append(wrappedOutbounds, singbox.NewOutboundWrapper(sbOut))
						break
					}
				}
			}
		}

		if len(wrappedOutbounds) == 0 {
			println("no working profiles left")
			break
		}

		println(fmt.Sprintf("round %d/%d", i+1, rounds))

		printerChan := make(chan testers.LatencyTestResult, len(wrappedOutbounds))
		defer close(printerChan)
		printer := printers.NewStatsPrinter(len(wrappedOutbounds), printerChan)
		printDone := make(chan bool)
		go printer.Start(printDone)

		sett := testers.NewLatencyTestSettings()
		sett.Timeout = 10 * time.Second

		// Convert outbounds to ProxyInfo and DialerFunc for generic testers
		var proxies []testers.ProxyInfo
		var dialers []testers.DialerFunc
		for _, outbound := range wrappedOutbounds {
			proxies = append(proxies, singbox.OutboundToProxyInfo(outbound))
			dialers = append(dialers, singbox.CreateDialerFunc(outbound, nil))
		}
		tlsConfigProvider := singbox.CreateTLSConfigProvider()

		lt, err := testers.NewLatencyTest(latencyTestCtx, sett, proxies, dialers, tlsConfigProvider)
		if err != nil {
			println(err.Error())
			continue
		}

		ltResChan := make(chan testers.LatencyTestResult, len(wrappedOutbounds))
		defer close(ltResChan)
		lt.Run(ltResChan, printerChan)

		results = nil
		for range len(wrappedOutbounds) {
			r := <-ltResChan
			if r.Error == nil {
				results = append(results, r)
			}
		}

		<-printDone
	}

	if len(results) == 0 {
		println("no good results")
		os.Exit(-1)
	}

	sortedResults := make([]testers.LatencyTestResult, 0, len(results))
	for _, r := range results {
		if r.Error == nil {
			sortedResults = append(sortedResults, r)
		}
	}

	slices.SortFunc(sortedResults, func(a, b testers.LatencyTestResult) int {
		if a.Delay < b.Delay {
			return -1
		}
		if a.Delay > b.Delay {
			return 1
		}
		return 0
	})

	var filteredOutbounds []core.Outbound
	for _, r := range sortedResults {
		// Find the outbound by tag
		sbOutbounds := instance.Outbound().Outbounds()
		for _, sbOut := range sbOutbounds {
			if sbOut.Tag() == r.Tag {
				filteredOutbounds = append(filteredOutbounds, singbox.NewOutboundWrapper(sbOut))
				break
			}
		}
	}

	success := 0

	f, _ := os.Create("out.txt")
	w := bufio.NewWriter(f)
	for _, r := range sortedResults {
		if r.Error == nil {
			success++
			i := slices.IndexFunc(profiles, func(p parsers.ProxyProfile) bool {
				return p.Config.Tag == r.Tag
			})
			if i == -1 {
				i = 0
			}
			w.WriteString(profiles[i].ConnURI + "\n")
		}
	}
	w.Flush()

	fmt.Printf("success %d\n", success)

	// Speed(ctx, filteredOutbounds, true)

	fmt.Println("Shutting down...")
	instance.Close()
}

func Speed(ctx context.Context, o []core.Outbound, upl bool) {
	var ts testers.SpeedTestSettings
	if !upl {
		ts = testers.NewDownloadTestSettings()
	} else {
		ts = testers.NewUploadTestSettings()
	}

	ts.TargetBytes = 10 * 1024 * 1024
	ts.Timeout = 10 * time.Second

	for i, outbound := range o {
		if i > 10 {
			// break
		}

		testCtx, testCtxCancel := context.WithCancel(ctx)
		defer testCtxCancel()

		// go func() {
		// 	time.Sleep(2 * time.Second)
		// 	testCtxCancel()
		// }()

		resChan := make(chan testers.SpeedTestResult)
		defer close(resChan)

		// Convert outbound to ProxyInfo and DialerFunc for generic testers
		proxies := []testers.ProxyInfo{singbox.OutboundToProxyInfo(outbound)}
		dialers := []testers.DialerFunc{singbox.CreateDialerFunc(outbound, nil)}
		tlsConfigProvider := singbox.CreateTLSConfigProvider()

		st, err := testers.NewSpeedTest(testCtx, ts, proxies, dialers, tlsConfigProvider)

		st.Run(resChan)
		r := <-resChan

		if err == nil {
			var t string
			if !upl {
				t = "download"
			} else {
				t = "upload"
			}
			if r.Error == nil {
				fmt.Printf("%s: %.2f MB/s\n", t, r.Speed/1024/1024)
			} else {
				fmt.Printf("%s: %s\n", t, r.Error.Error())
			}
		} else {
			println(err.Error())
		}
	}
}
