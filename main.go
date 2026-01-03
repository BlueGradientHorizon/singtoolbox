package main

import (
	"bufio"
	"context"
	"fmt"
	"net/netip"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/bluegradienthorizon/singtoolbox/parsers"
	"github.com/bluegradienthorizon/singtoolbox/printers"
	"github.com/bluegradienthorizon/singtoolbox/testers"
	"github.com/bluegradienthorizon/singtoolbox/tools"
	"github.com/bluegradienthorizon/singtoolbox/utils"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/json/badoption"
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

	validationErrorsMap := make(map[string]int)

	i := 0
	for _, p := range profiles {
		ctx := include.Context(context.Background())
		instance, err := box.New(box.Options{
			Context: ctx,
			Options: option.Options{
				Outbounds: []option.Outbound{*p.Outbound},
			},
		})
		if err != nil {
			validationErrorsMap[p.Outbound.Type+": "+err.Error()]++
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
		(&profiles[i]).Outbound.Tag = fmt.Sprintf("outbound-%d", i)
	}

	ctx := include.Context(context.Background())

	var outbounds []option.Outbound
	for _, p := range profiles {
		outbounds = append(outbounds, *p.Outbound)
	}

	opts := option.Options{
		Log: &option.LogOptions{
			Level:     "panic",
			Timestamp: true,
		},
		Inbounds: []option.Inbound{
			{
				Type: "socks",
				Tag:  "socks-in",
				Options: &option.SocksInboundOptions{
					ListenOptions: option.ListenOptions{
						Listen:     common.Ptr(badoption.Addr(netip.IPv4Unspecified())),
						ListenPort: 1080,
					},
				},
			},
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
		var outbounds []adapter.Outbound
		if i == 0 {
			outbounds = instance.Outbound().Outbounds()
		} else {
			for _, r := range results {
				outbounds = append(outbounds, r.Outbound)
			}
		}

		if len(outbounds) == 0 {
			println("no working configs left")
			break
		}

		println(fmt.Sprintf("round %d/%d", i+1, rounds))

		printer := printers.NewStatsPrinter(len(outbounds))
		resChan := printer.ResultChan()
		printDone := make(chan bool)
		go printer.Start(printDone)

		sett := testers.NewLatencyTestSettings()
		sett.Timeout = 30 * time.Second
		res := testers.LatencyTest(latencyTestCtx, sett, outbounds, resChan)

		results = results[:0]
		for _, r := range res {
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

	var filteredOutbounds []adapter.Outbound
	for _, r := range sortedResults {
		filteredOutbounds = append(filteredOutbounds, r.Outbound)
	}

	success := 0

	f, _ := os.Create("out.txt")
	w := bufio.NewWriter(f)
	for _, r := range sortedResults {
		if r.Error == nil {
			success++
			i := slices.IndexFunc(profiles, func(p parsers.ProxyProfile) bool {
				return p.Outbound.Tag == r.Tag
			})
			if i == -1 {
				i = 0
			}
			w.WriteString(profiles[i].ConnURI + "\n")
		}
	}
	w.Flush()

	fmt.Printf("success %d\n", success)

	// for i, o := range filteredOutbounds {
	// 	downloadTestCtx, downloadTestCtxCancel := context.WithCancel(ctx)
	// 	defer downloadTestCtxCancel()

	// 	go func() {
	// 		time.Sleep(2 * time.Second)
	// 		downloadTestCtxCancel()
	// 	}()

	// 	if i > 10 {
	// 		// break
	// 	}

	// 	dts := testers.NewDownloadTestSettings()
	// 	dts.TargetBytes = 10 * 1024 * 1024
	// 	dts.Timeout = 15 * time.Second
	// 	dst, err := testers.SpeedTest(
	// 		downloadTestCtx,
	// 		dts,
	// 		[]adapter.Outbound{o},
	// 		nil,
	// 	)

	// 	if err == nil {
	// 		if dst[0].Error == nil {
	// 			fmt.Printf("download: %.2f MB/s\n", dst[0].Speed/1024/1024)
	// 		} else {
	// 			println("download: " + dst[0].Error.Error())
	// 		}
	// 	} else {
	// 		println(err.Error())
	// 	}
	// }

	// for i, o := range filteredOutbounds {
	// 	uploadTestCtx, uploadTestCtxCancel := context.WithCancel(ctx)
	// 	defer uploadTestCtxCancel()

	// 	go func() {
	// 		time.Sleep(2 * time.Second)
	// 		uploadTestCtxCancel()
	// 	}()

	// 	if i > 10 {
	// 		// break
	// 	}

	// 	uts := testers.NewUploadTestSettings()
	// 	uts.TargetBytes = 10 * 1024 * 1024
	// 	uts.Timeout = 15 * time.Second
	// 	ust, err := testers.SpeedTest(
	// 		uploadTestCtx,
	// 		uts,
	// 		[]adapter.Outbound{o},
	// 		nil,
	// 	)

	// 	if err == nil {
	// 		if ust[0].Error == nil {
	// 			fmt.Printf("upload: %.2f MB/s\n", ust[0].Speed/1024/1024)
	// 		} else {
	// 			println("upload: " + ust[0].Error.Error())
	// 		}
	// 	} else {
	// 		println(err.Error())
	// 	}
	// }

	fmt.Println("Shutting down...")
	instance.Close()
}
