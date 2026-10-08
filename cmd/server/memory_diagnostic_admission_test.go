package main

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"
)

func TestHandlePerfMemoryDiagnosticRejectsConcurrentScan(t *testing.T) {
	srv := NewServer(nil, &Config{}, nil)
	srv.store = &PacketStore{}

	// Hold the store write lock so the first admitted request remains in the
	// diagnostic path while the second request attempts admission.
	srv.store.mu.Lock()
	storeLocked := true
	t.Cleanup(func() {
		if storeLocked {
			srv.store.mu.Unlock()
		}
	})

	firstResponse := httptest.NewRecorder()
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		srv.handlePerf(firstResponse, httptest.NewRequest(http.MethodGet, "/api/perf?mem=1", nil))
	}()

	deadline := time.Now().Add(time.Second)
	for !srv.memoryDiagnosticActive.Load() && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if !srv.memoryDiagnosticActive.Load() {
		t.Fatal("first memory diagnostic was not admitted")
	}

	secondResponse := httptest.NewRecorder()
	srv.handlePerf(secondResponse, httptest.NewRequest(http.MethodGet, "/api/perf?mem=1", nil))
	if secondResponse.Code != http.StatusTooManyRequests {
		t.Fatalf("concurrent diagnostic status: want %d, got %d", http.StatusTooManyRequests, secondResponse.Code)
	}
	if got := secondResponse.Header().Get("Retry-After"); got != "30" {
		t.Fatalf("concurrent diagnostic Retry-After: want %q, got %q", "30", got)
	}

	srv.store.mu.Unlock()
	storeLocked = false
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("first memory diagnostic did not complete")
	}
	if firstResponse.Code != http.StatusOK {
		t.Fatalf("first diagnostic status: want %d, got %d", http.StatusOK, firstResponse.Code)
	}
}

func TestHandlePerfMemoryDiagnosticRejectsImmediateSequentialRetry(t *testing.T) {
	srv := NewServer(nil, &Config{}, nil)
	srv.store = &PacketStore{}

	firstResponse := httptest.NewRecorder()
	srv.handlePerf(firstResponse, httptest.NewRequest(http.MethodGet, "/api/perf?mem=1", nil))
	if firstResponse.Code != http.StatusOK {
		t.Fatalf("first diagnostic status: want %d, got %d", http.StatusOK, firstResponse.Code)
	}

	secondResponse := httptest.NewRecorder()
	srv.handlePerf(secondResponse, httptest.NewRequest(http.MethodGet, "/api/perf?mem=1", nil))
	if secondResponse.Code != http.StatusTooManyRequests {
		t.Fatalf("immediate retry status: want %d, got %d", http.StatusTooManyRequests, secondResponse.Code)
	}
	if got := secondResponse.Header().Get("Retry-After"); got != "30" {
		t.Fatalf("immediate retry Retry-After: want %q, got %q", "30", got)
	}
}

func TestHandlePerfMemoryDiagnosticAdmitsRequestAfterCooldown(t *testing.T) {
	srv := NewServer(nil, &Config{}, nil)
	srv.store = &PacketStore{}
	now := time.Date(2026, time.September, 18, 12, 0, 0, 0, time.UTC)
	srv.memoryDiagnosticNow = func() time.Time { return now }

	firstResponse := httptest.NewRecorder()
	srv.handlePerf(firstResponse, httptest.NewRequest(http.MethodGet, "/api/perf?mem=1", nil))
	if firstResponse.Code != http.StatusOK {
		t.Fatalf("first diagnostic status: want %d, got %d", http.StatusOK, firstResponse.Code)
	}

	now = now.Add(30 * time.Second)
	laterResponse := httptest.NewRecorder()
	srv.handlePerf(laterResponse, httptest.NewRequest(http.MethodGet, "/api/perf?mem=1", nil))
	if laterResponse.Code != http.StatusOK {
		t.Fatalf("post-cooldown diagnostic status: want %d, got %d", http.StatusOK, laterResponse.Code)
	}
}
