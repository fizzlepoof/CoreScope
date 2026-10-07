package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestStaticTraceHandlerRecordsRequestLifecycle(t *testing.T) {
	dir := t.TempDir()
	tracePath := filepath.Join(dir, "static-trace.jsonl")
	handler, closeTrace, err := staticTraceHandler(tracePath, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("console.log('trace');"))
	}))
	if err != nil {
		t.Fatalf("staticTraceHandler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "http://corescope.test/customize-v2.js", nil)
	req.RemoteAddr = "127.0.0.1:41000"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	closeTrace()

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}

	contents, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	var event staticTraceEvent
	if err := json.Unmarshal(contents, &event); err != nil {
		t.Fatalf("decode trace: %v", err)
	}
	if event.Method != http.MethodGet || event.Path != "/customize-v2.js" {
		t.Fatalf("request = %s %s, want GET /customize-v2.js", event.Method, event.Path)
	}
	if event.RemoteAddr != "127.0.0.1:41000" {
		t.Fatalf("remote address = %q", event.RemoteAddr)
	}
	if event.Status != http.StatusOK || event.Bytes != int64(len("console.log('trace');")) {
		t.Fatalf("response = status %d bytes %d", event.Status, event.Bytes)
	}
	if event.ElapsedMicros < 0 || event.FinishedAt.IsZero() {
		t.Fatalf("invalid lifecycle timing: %+v", event)
	}
}
