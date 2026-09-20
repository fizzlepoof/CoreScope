package main

import (
	"testing"

	"github.com/meshcore-analyzer/pprofconfig"
)

func TestPprofListenAddressDefaultsToLoopback(t *testing.T) {
	getenv := func(string) string { return "" }
	if got := pprofconfig.IngestorAddress(getenv); got != "127.0.0.1:6061" {
		t.Fatalf("pprof default address = %q, want loopback-only address", got)
	}
}

func TestPprofListenAddressAllowsExplicitHostAndPort(t *testing.T) {
	values := map[string]string{
		"PPROF_HOST":          "0.0.0.0",
		"INGESTOR_PPROF_PORT": "7071",
	}
	getenv := func(key string) string { return values[key] }
	if got := pprofconfig.IngestorAddress(getenv); got != "0.0.0.0:7071" {
		t.Fatalf("pprof override address = %q, want explicit host and port", got)
	}
}
