package printers

import (
	"fmt"
	"github.com/bluegradienthorizon/singtoolbox/testers"
)

type StatsPrinter struct {
	total     int
	completed int
	succeeded int
	failed    int
	results   chan testers.LatencyTestResult
}

func NewStatsPrinter(total int) *StatsPrinter {
	return &StatsPrinter{
		total:   total,
		results: make(chan testers.LatencyTestResult, 100),
	}
}

func (s *StatsPrinter) ResultChan() chan<- testers.LatencyTestResult {
	return s.results
}

func (s *StatsPrinter) Start(done chan<- bool) {
	for result := range s.results {
		s.completed++
		if result.Error == nil {
			s.succeeded++
		} else {
			s.failed++
		}
		s.printStats()

		if s.completed == s.total {
			break
		}
	}
	fmt.Println()
	done <- true
}

func (s *StatsPrinter) printStats() {
	running := s.total - s.completed
	fmt.Printf("\rRunning: %-4d | Succeeded: %-4d | Failed: %-4d | Total: %d",
		running, s.succeeded, s.failed, s.total)
}
