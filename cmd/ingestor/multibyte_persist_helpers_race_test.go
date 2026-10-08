package main

import (
	"log"
	"strings"
	"sync"
	"testing"
)

// Exercise the actual standard logger and captureLogs buffer concurrently,
// as async migrations do while malformed-snapshot tests inspect their logs.
func TestCaptureLogs_ConcurrentReadWrite(t *testing.T) {
	buf := captureLogs(t)
	const marker = "capture-logs-concurrent-marker"
	const iterations = 1000
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < iterations; i++ {
			log.Print(marker)
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < iterations; i++ {
			_ = logContains(buf, marker)
			_ = buf.String()
		}
	}()
	close(start)
	wg.Wait()
	if got := strings.Count(buf.String(), marker); got != iterations {
		t.Fatalf("captured marker count: got %d, want %d", got, iterations)
	}
	if !logContains(buf, strings.ToUpper(marker)) {
		t.Fatal("case-insensitive logContains lost captured marker")
	}
}

func TestCaptureLogs_IndependentBuffers(t *testing.T) {
	previousWriter := log.Writer()
	previousFlags := log.Flags()
	t.Run("first", func(t *testing.T) {
		buf := captureLogs(t)
		log.Print("first-capture-marker")
		if !logContains(buf, "first-capture-marker") {
			t.Fatal("first capture missing marker")
		}
	})
	t.Run("second", func(t *testing.T) {
		buf := captureLogs(t)
		if buf.String() != "" {
			t.Fatal("new capture retained previous test's logs")
		}
		log.Print("second-capture-marker")
		if !logContains(buf, "second-capture-marker") || logContains(buf, "first-capture-marker") {
			t.Fatal("captures are not independent")
		}
	})
	if log.Writer() != previousWriter || log.Flags() != previousFlags {
		t.Fatal("capture cleanup did not restore logger writer and flags")
	}
}
