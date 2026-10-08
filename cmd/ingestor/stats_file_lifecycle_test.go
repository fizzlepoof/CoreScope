package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Keep the pre-fix API compilable: RED must be a runtime contract failure,
// not a compilation error caused by assigning the former void return.
func statsWriterStarter(t *testing.T) func(*Store, time.Duration) func() {
	t.Helper()
	start, ok := interface{}(StartStatsFileWriter).(func(*Store, time.Duration) func())
	if !ok {
		t.Fatal("StartStatsFileWriter must return a stop function that joins its worker")
	}
	return start
}

func awaitStatsWriter(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestStatsFileWriter_BindsSamplerAndPathAndJoinsConcurrentStops(t *testing.T) {
	start := statsWriterStarter(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "stats.json")
	otherPath := filepath.Join(dir, "other.json")
	t.Setenv("CORESCOPE_INGESTOR_STATS", path)
	store, err := OpenStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	original := readProcSelfIOFn
	defer func() { readProcSelfIOFn = original }()

	initialEntered := make(chan struct{})
	initialRelease := make(chan struct{})
	tickEntered := make(chan struct{})
	tickRelease := make(chan struct{})
	var releaseInitial, releaseTick sync.Once
	var calls atomic.Int64
	var replacementCalls atomic.Int64
	readProcSelfIOFn = func() procIOSnapshot {
		n := calls.Add(1)
		if n == 1 {
			close(initialEntered)
			<-initialRelease
		} else if n == 2 {
			close(tickEntered)
			<-tickRelease
		}
		return procIOSnapshot{
			at: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(n) * time.Second),
			readBytes: 1000 * n, writeBytes: 2000 * n, syscR: 10 * n, syscW: 20 * n, ok: true,
		}
	}
	stop := start(store, 10*time.Millisecond)
	defer func() {
		releaseInitial.Do(func() { close(initialRelease) })
		releaseTick.Do(func() { close(tickRelease) })
		stop()
	}()
	awaitStatsWriter(t, initialEntered, "initial sampler entry")
	// The active instance must never adopt another writer's sampler/path.
	readProcSelfIOFn = func() procIOSnapshot {
		replacementCalls.Add(1)
		return procIOSnapshot{}
	}
	t.Setenv("CORESCOPE_INGESTOR_STATS", otherPath)
	releaseInitial.Do(func() { close(initialRelease) })
	awaitStatsWriter(t, tickEntered, "bound tick sampler entry")

	const stoppers = 8
	attempted := make(chan struct{}, stoppers)
	returned := make(chan struct{}, stoppers)
	for i := 0; i < stoppers; i++ {
		go func() {
			attempted <- struct{}{}
			stop()
			returned <- struct{}{}
		}()
	}
	for i := 0; i < stoppers; i++ {
		awaitStatsWriter(t, attempted, "stop caller start")
	}
	// The sampler is held at a channel barrier. No stop caller may return
	// while the worker is still in that in-flight tick (even repeat callers).
	select {
	case <-returned:
		t.Error("stop returned before the in-flight sampler and disk write completed")
	case <-time.After(100 * time.Millisecond):
	}
	releaseTick.Do(func() { close(tickRelease) })
	for i := 0; i < stoppers; i++ {
		awaitStatsWriter(t, returned, "joined stop return")
	}
	stop() // sequential idempotence after concurrent stop/join

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("joined writer must complete its in-flight disk publication: %v", err)
	}
	var snap IngestorStatsSnapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.ProcIO == nil || snap.ProcIO.ReadBytesPerSec != 1000 || snap.ProcIO.WriteBytesPerSec != 2000 || snap.ProcIO.SyscallsRead != 10 || snap.ProcIO.SyscallsWrite != 20 {
		t.Fatalf("bound sampler rates lost: %+v", snap.ProcIO)
	}
	if snap.SampledAt != snap.ProcIO.SampledAt {
		t.Fatalf("timestamp bytes differ: %q != %q", snap.SampledAt, snap.ProcIO.SampledAt)
	}
	if _, err := os.Stat(otherPath); !os.IsNotExist(err) {
		t.Fatalf("writer adopted replacement path: %v", err)
	}
	if got := replacementCalls.Load(); got != 0 {
		t.Fatalf("writer adopted replacement sampler: %d calls", got)
	}
	t.Logf("joined real disk publication: bound sampler calls=%d replacement calls=%d; procIO rates=1000/2000/10/20; timestamp bytes equal", calls.Load(), replacementCalls.Load())
}

func TestStatsFileWriter_StopBeforeFirstTick(t *testing.T) {
	start := statsWriterStarter(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "stats.json")
	t.Setenv("CORESCOPE_INGESTOR_STATS", path)
	store, err := OpenStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	stop := start(store, time.Hour)
	stop()
	stop()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stop before first tick unexpectedly published: %v", err)
	}
}
