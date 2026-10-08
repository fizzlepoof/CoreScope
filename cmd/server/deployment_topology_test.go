package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func readRepoFile(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "..", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestProductionNoMosquittoComposePropagatesDisableCaddy(t *testing.T) {
	compose := stripYAMLComments(readRepoFile(t, "docker-compose.no-mosquitto.yml"))
	if !strings.Contains(compose, "DISABLE_CADDY=${DISABLE_CADDY:-false}") {
		t.Fatal("docker-compose.no-mosquitto.yml must propagate DISABLE_CADDY with the production false default")
	}
}

func TestProductionBundledBrokerPublishesOnlyOnHostLoopback(t *testing.T) {
	compose := stripYAMLComments(readRepoFile(t, "docker-compose.yml"))
	want := `"127.0.0.1:${PROD_MQTT_PORT:-1883}:1883"`
	if !strings.Contains(compose, want) {
		t.Fatalf("bundled production MQTT port must use host-loopback-only mapping %s", want)
	}
	if strings.Contains(compose, `"${PROD_MQTT_PORT:-1883}:1883"`) {
		t.Fatal("bundled production MQTT port must not publish on all host interfaces")
	}
}

func TestExampleAndNoMosquittoCommentsDescribeSecureDefaults(t *testing.T) {
	example := readRepoFile(t, "docker-compose.example.yml")
	if !strings.Contains(example, "broker\n# DISABLED by default") || !strings.Contains(example, "DISABLE_MOSQUITTO=${DISABLE_MOSQUITTO:-true}") {
		t.Fatal("example compose comments and environment must agree that bundled Mosquitto is disabled by default")
	}

	noMosquitto := readRepoFile(t, "docker-compose.no-mosquitto.yml")
	if strings.Contains(noMosquitto, "keeps the built-in broker") {
		t.Fatal("no-mosquitto compose must not claim the example compose keeps the built-in broker")
	}
	if !strings.Contains(noMosquitto, "docker-compose.example.yml") || !strings.Contains(noMosquitto, "built-in broker disabled by default") {
		t.Fatal("no-mosquitto compose must describe the example compose's secure broker default")
	}
}

func TestManageStandardStagingAlwaysUsesCanonicalExternalBrokerCompose(t *testing.T) {
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	for _, disableMosquitto := range []string{"true", "false"} {
		t.Run("production_disable_mosquitto_"+disableMosquitto, func(t *testing.T) {
			binDir := t.TempDir()
			logPath := filepath.Join(t.TempDir(), "docker.log")
			fakeDocker := filepath.Join(binDir, "docker")
			script := "#!/bin/sh\n" +
				"printf '%s\\n' \"$*\" >> \"$DOCKER_LOG\"\n" +
				"exit 0\n"
			if err := os.WriteFile(fakeDocker, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}

			cmd := exec.Command("bash", "manage.sh", "stop", "staging")
			cmd.Dir = repo
			cmd.Env = append(os.Environ(),
				"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"DOCKER_LOG="+logPath,
				"DISABLE_MOSQUITTO="+disableMosquitto,
			)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("manage stop staging: %v\n%s", err, output)
			}
			logged, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			calls := string(logged)
			if !strings.Contains(calls, "compose -f docker-compose.staging.yml -p corescope-staging rm -sf staging-go") {
				t.Fatalf("standard staging must select canonical external-broker compose; docker calls:\n%s", calls)
			}
			if strings.Contains(calls, "docker-compose.staging.no-mosquitto.yml") {
				t.Fatalf("production DISABLE_MOSQUITTO must not select the legacy staging compose; docker calls:\n%s", calls)
			}
		})
	}
}

func TestManageStagingMessagesDoNotAdvertiseNonexistentMQTTPorts(t *testing.T) {
	manage := readRepoFile(t, "manage.sh")
	if strings.Contains(manage, "STAGING_GO_MQTT_PORT") {
		t.Fatal("manage.sh must not advertise a staging MQTT host port that canonical staging does not publish")
	}
	if !strings.Contains(manage, "Staging started on port ${STAGING_GO_HTTP_PORT:-82} (external MQTT broker)") {
		t.Fatal("staging start message must identify the external broker topology")
	}
}

func TestExternalBrokerComposeVariantsPropagateMQTTBroker(t *testing.T) {
	for _, name := range []string{"docker-compose.yml", "docker-compose.example.yml", "docker-compose.no-mosquitto.yml"} {
		t.Run(name, func(t *testing.T) {
			compose := stripYAMLComments(readRepoFile(t, name))
			if !strings.Contains(compose, "MQTT_BROKER=${MQTT_BROKER:-}") {
				t.Fatalf("%s must pass the operator's MQTT_BROKER into the container", name)
			}
		})
	}
}

func TestStagingNoMosquittoComposeRequiresExternalBrokerAndNeverEnablesBundledBroker(t *testing.T) {
	compose := stripYAMLComments(readRepoFile(t, "docker-compose.staging.no-mosquitto.yml"))
	block := extractStagingGoBlock(t, compose)
	envBlock := extractSubBlock(block, "environment", 4)

	if !strings.Contains(envBlock, "MQTT_BROKER=${MQTT_BROKER:?set MQTT_BROKER to an external broker URL}") {
		t.Fatalf("staging no-mosquitto variant must require and pass an external MQTT_BROKER; env block:\n%s", envBlock)
	}
	if !strings.Contains(envBlock, "DISABLE_MOSQUITTO=true") {
		t.Fatalf("staging no-mosquitto variant must force bundled Mosquitto off; env block:\n%s", envBlock)
	}
	if strings.Contains(envBlock, "${DISABLE_MOSQUITTO") {
		t.Fatalf("staging no-mosquitto variant must not permit an override to enable bundled Mosquitto; env block:\n%s", envBlock)
	}
	portsBlock := extractSubBlock(block, "ports", 4)
	if strings.Contains(portsBlock, ":1883") {
		t.Fatalf("staging no-mosquitto variant must not publish the bundled MQTT port; ports block:\n%s", portsBlock)
	}
}

func TestActiveDeploymentDocsDoNotRetainInsecureMQTTClaims(t *testing.T) {
	stale := map[string][]string{
		"README.md":          {"Open `http://localhost` — done. No config file needed; CoreScope starts with sensible defaults."},
		"DEPLOY.md":          {"Keep using manage.sh (no changes needed)", "Nothing breaks."},
		"docs/deployment.md": {"Built-in defaults — work out of the box with no config"},
		"docs/DEPLOYMENT.md": {"Built-in MQTT broker for receiving packets from observers", "The rest of the defaults work out of the box.", "The container runs its own MQTT broker"},
	}
	for name, claims := range stale {
		text := readRepoFile(t, name)
		for _, claim := range claims {
			if strings.Contains(text, claim) {
				t.Errorf("%s retains stale active-operator claim %q", name, claim)
			}
		}
	}

	for _, name := range []string{"README.md", "DEPLOY.md", "docs/deployment.md", "docs/DEPLOYMENT.md"} {
		text := readRepoFile(t, name)
		if !strings.Contains(text, "MQTT_BROKER") || !strings.Contains(text, "DISABLE_MOSQUITTO=false") {
			t.Errorf("%s must document both the external-broker path and explicit bundled-broker opt-in", name)
		}
	}
}

func TestActiveDeploymentDocsDescribeCurrentRuntimeTopology(t *testing.T) {
	deploy := readRepoFile(t, "DEPLOY.md")
	for _, stale := range []string{
		"Reverting to the old single-container behaviour",
		"restore the `1883:1883` mapping",
		"That gives you back the pre-v3.7 self-contained staging shape",
	} {
		if strings.Contains(deploy, stale) {
			t.Errorf("DEPLOY.md retains broken or insecure staging rollback guidance %q", stale)
		}
	}

	architecture := readRepoFile(t, "docs/DEPLOYMENT.md")
	for _, stale := range []string{
		"N[Node.js]",
		"N[Node.js server]",
		"participant N as Node.js",
		"docker exec corescope mosquitto_sub",
		"You don't install Node.js, Mosquitto, or Caddy separately; they're all included in the container.",
		"proxy directly to the Node.js port",
	} {
		if strings.Contains(architecture, stale) {
			t.Errorf("docs/DEPLOYMENT.md retains obsolete runtime architecture %q", stale)
		}
	}
	for _, current := range []string{"External MQTT broker", "Go ingestor", "Go server", "SQLite"} {
		if !strings.Contains(architecture, current) {
			t.Errorf("docs/DEPLOYMENT.md current architecture is missing %q", current)
		}
	}
}

func TestIngestorReadmeDescribesCurrentGoTopology(t *testing.T) {
	readme := readRepoFile(t, "cmd/ingestor/README.md")
	for _, stale := range []string{
		"Node.js web server",
		"Node.js Web Server",
		"Runs **alongside** the Node.js server",
		"that stays in Node.js",
		"same v3 schema as the Node.js server",
		"Both processes can write to the same DB concurrently",
		"These stay in the Node.js server for now",
	} {
		if strings.Contains(readme, stale) {
			t.Errorf("cmd/ingestor/README.md retains obsolete architecture %q", stale)
		}
	}
	for _, current := range []string{
		"Go Ingestor",
		"Go Server",
		"SQLite DB",
		"All database writes are owned by the ingestor",
		"server opens SQLite read-only",
	} {
		if !strings.Contains(readme, current) {
			t.Errorf("cmd/ingestor/README.md current architecture is missing %q", current)
		}
	}
}
