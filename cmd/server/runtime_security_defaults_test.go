package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meshcore-analyzer/pprofconfig"
)

func TestPprofListenAddressDefaultsToLoopback(t *testing.T) {
	getenv := func(string) string { return "" }
	if got := pprofconfig.ServerAddress(getenv); got != "127.0.0.1:6060" {
		t.Fatalf("pprof default address = %q, want loopback-only address", got)
	}
}

func TestPprofListenAddressAllowsExplicitHostAndPort(t *testing.T) {
	values := map[string]string{
		"PPROF_HOST":        "0.0.0.0",
		"SERVER_PPROF_PORT": "7070",
	}
	getenv := func(key string) string { return values[key] }
	if got := pprofconfig.ServerAddress(getenv); got != "0.0.0.0:7070" {
		t.Fatalf("pprof override address = %q, want explicit host and port", got)
	}
}

func TestDeploymentSurfacesDisableBuiltInMQTTByDefault(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	checks := []struct {
		path string
		want string
	}{
		{"docker-compose.yml", "DISABLE_MOSQUITTO=${DISABLE_MOSQUITTO:-true}"},
		{"docker-compose.example.yml", "DISABLE_MOSQUITTO=${DISABLE_MOSQUITTO:-true}"},
		{".env.example", "DISABLE_MOSQUITTO=true"},
		{"docker/entrypoint-go.sh", "${DISABLE_MOSQUITTO:-true}"},
		{"manage.sh", "${DISABLE_MOSQUITTO:-true}"},
	}

	for _, check := range checks {
		t.Run(check.path, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(repoRoot, check.path))
			if err != nil {
				t.Fatalf("read %s: %v", check.path, err)
			}
			if !strings.Contains(string(data), check.want) {
				t.Fatalf("%s must contain secure MQTT default %q", check.path, check.want)
			}
		})
	}
}

func TestDockerBuildCopiesSharedPprofConfigForBothBinaries(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "Dockerfile"))
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	const copyLine = "COPY internal/pprofconfig/ ../../internal/pprofconfig/"
	if got := strings.Count(string(data), copyLine); got != 2 {
		t.Fatalf("Dockerfile pprofconfig copy count = %d, want 2 (server and ingestor)", got)
	}
}

func TestStagingPprofPortsPublishOnHostLoopbackOnly(t *testing.T) {
	for _, name := range []string{"docker-compose.staging.yml", "docker-compose.staging.no-mosquitto.yml"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", name))
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			text := string(data)
			for _, mapping := range []string{"127.0.0.1:6060:6060", "127.0.0.1:6061:6061"} {
				if !strings.Contains(text, mapping) {
					t.Errorf("%s must publish pprof via host loopback mapping %q", name, mapping)
				}
			}
			if !strings.Contains(text, "PPROF_HOST=0.0.0.0") {
				t.Errorf("%s must explicitly opt into container-interface binding for loopback-published pprof", name)
			}
			for _, setting := range []string{"SERVER_PPROF_PORT=6060", "INGESTOR_PPROF_PORT=6061"} {
				if !strings.Contains(text, setting) {
					t.Errorf("%s must configure process-specific pprof setting %q", name, setting)
				}
			}
		})
	}
}
