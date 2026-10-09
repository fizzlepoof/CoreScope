package main

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Exercise the actual test-loop owner with an in-flight production callback.
// A test must not return and expose its registry to the next test before its
// watchdog has stopped. The delayed release controls work, not a performance
// assertion; completion is observed through the real loop's exited channel.
func TestWatchdogTestLoopCleanupJoinsInFlightTick(t *testing.T) {
	defer snapshotAndResetRegistry(t)()
	entered := make(chan struct{})
	release := make(chan struct{})
	cleanupStarted := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	var done chan struct{}
	var exited chan struct{}
	t.Cleanup(func() {
		unblock()
		if done != nil {
			select {
			case <-done:
			default:
				close(done)
			}
		}
		if exited != nil {
			select {
			case <-exited:
			case <-time.After(time.Second):
				t.Error("test watchdog did not stop after releasing its callback")
			}
		}
	})
	go func() {
		<-cleanupStarted
		// Keep the production callback in flight across cleanup entry.
		time.Sleep(100 * time.Millisecond)
		unblock()
	}()
	t.Run("owner", func(t *testing.T) {
		s := &SourceLivenessState{Tag: "owned-loop", IsConnectedFn: func() bool { return true }}
		atomic.StoreInt64(&s.LastMessageUnix, time.Now().Add(-time.Hour).Unix())
		if err := registerLivenessState(s); err != nil {
			t.Fatal(err)
		}
		tick, stop, joined := setupWatchdogTestLoop(t, time.Minute, func(...any) {
			close(entered)
			<-release
		})
		done, exited = stop, joined
		t.Cleanup(func() { close(cleanupStarted) })
		sendTickOrFail(t, tick, time.Now(), time.Second, "controlled in-flight tick")
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("watchdog did not enter the real callback")
		}
	})
	select {
	case <-exited:
	default:
		t.Error("test owner returned before its in-flight watchdog tick was joined")
	}
}

func TestWatchdogTestLoopCleanupStopsIdleLoop(t *testing.T) {
	defer snapshotAndResetRegistry(t)()
	var exited chan struct{}
	var done chan struct{}
	t.Cleanup(func() {
		select {
		case <-done:
		default:
			close(done)
		}
		select {
		case <-exited:
		case <-time.After(time.Second):
			t.Error("idle watchdog did not stop")
		}
	})
	t.Run("owner", func(t *testing.T) {
		_, done, exited = setupWatchdogTestLoop(t, time.Minute, func(...any) {})
	})
	select {
	case <-exited:
	default:
		t.Error("test owner returned with its idle watchdog still running")
	}
}
