package pprofconfig

import "testing"

func getenv(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestProcessAddressesUseSpecificPortsAndDefaults(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		env := getenv(nil)
		if got := ServerAddress(env); got != "127.0.0.1:6060" {
			t.Fatalf("ServerAddress() = %q, want %q", got, "127.0.0.1:6060")
		}
		if got := IngestorAddress(env); got != "127.0.0.1:6061" {
			t.Fatalf("IngestorAddress() = %q, want %q", got, "127.0.0.1:6061")
		}
	})

	t.Run("process specific ports override legacy shared port", func(t *testing.T) {
		env := getenv(map[string]string{
			"PPROF_PORT":          "9999",
			"SERVER_PPROF_PORT":   "7060",
			"INGESTOR_PPROF_PORT": "7061",
		})
		if got := ServerAddress(env); got != "127.0.0.1:7060" {
			t.Fatalf("ServerAddress() = %q, want process-specific port", got)
		}
		if got := IngestorAddress(env); got != "127.0.0.1:7061" {
			t.Fatalf("IngestorAddress() = %q, want process-specific port", got)
		}
	})
}

func TestSimultaneousEnvironmentProducesDistinctAddresses(t *testing.T) {
	env := getenv(map[string]string{
		"PPROF_HOST":          "0.0.0.0",
		"SERVER_PPROF_PORT":   "6060",
		"INGESTOR_PPROF_PORT": "6061",
	})
	server := ServerAddress(env)
	ingestor := IngestorAddress(env)
	if server == ingestor {
		t.Fatalf("server and ingestor pprof addresses collide at %q", server)
	}
	if server != "0.0.0.0:6060" || ingestor != "0.0.0.0:6061" {
		t.Fatalf("addresses = %q, %q; want distinct configured addresses", server, ingestor)
	}
}

func TestEnabledRequiresExactTrue(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"", false},
		{"false", false},
		{"TRUE", false},
		{"True", false},
		{" true", false},
		{"true ", false},
		{"1", false},
		{"true", true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			if got := Enabled(getenv(map[string]string{"ENABLE_PPROF": tc.value})); got != tc.want {
				t.Fatalf("Enabled(ENABLE_PPROF=%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestAddressesFormatIPv6Host(t *testing.T) {
	env := getenv(map[string]string{
		"PPROF_HOST":          "::1",
		"SERVER_PPROF_PORT":   "7060",
		"INGESTOR_PPROF_PORT": "7061",
	})
	if got := ServerAddress(env); got != "[::1]:7060" {
		t.Fatalf("ServerAddress() = %q, want bracketed IPv6 address", got)
	}
	if got := IngestorAddress(env); got != "[::1]:7061" {
		t.Fatalf("IngestorAddress() = %q, want bracketed IPv6 address", got)
	}
}
