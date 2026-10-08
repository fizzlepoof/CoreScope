#!/usr/bin/env sh
set -eu

REPO_ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
HELPER="$REPO_ROOT/docker/entrypoint-env.sh"

grep -Fq 'COPY docker/entrypoint-env.sh /app/docker/entrypoint-env.sh' "$REPO_ROOT/Dockerfile" || {
  printf 'FAIL: Docker image does not include entrypoint env helper\n' >&2
  exit 1
}

awk '
  /source_data_env_preserving_runtime \/app\/data\/\.env/ { source_line = NR }
  /enforce_bundled_mqtt_broker/ { enforce_line = NR }
  END { exit !(source_line && enforce_line == source_line + 1) }
' "$REPO_ROOT/docker/entrypoint-go.sh" || {
  printf 'FAIL: bundled broker enforcement is not immediately after data env resolution\n' >&2
  exit 1
}

grep -Fq 'if is_true "${DISABLE_MOSQUITTO:-true}"' "$REPO_ROOT/docker/entrypoint-go.sh" || {
  printf 'FAIL: supervisor selection does not use the shared POSIX truth predicate\n' >&2
  exit 1
}

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT HUP INT TERM

env_file="$tmp_dir/data.env"
cat >"$env_file" <<'ENV'
DISABLE_MOSQUITTO=false
MQTT_BROKER=mqtt://data-broker:1883
DISABLE_CADDY=false
ENABLE_PPROF=false
PPROF_HOST=127.0.0.1
SERVER_PPROF_PORT=16060
INGESTOR_PPROF_PORT=16061
DATA_ONLY=loaded
ENV

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

(
  DISABLE_MOSQUITTO=true
  MQTT_BROKER=mqtts://runtime-broker.example:8883
  DISABLE_CADDY=true
  ENABLE_PPROF=true
  PPROF_HOST=
  SERVER_PPROF_PORT=6060
  INGESTOR_PPROF_PORT=6061
  export DISABLE_MOSQUITTO MQTT_BROKER DISABLE_CADDY ENABLE_PPROF PPROF_HOST SERVER_PPROF_PORT INGESTOR_PPROF_PORT

  . "$HELPER"
  source_data_env_preserving_runtime "$env_file"
  enforce_bundled_mqtt_broker

  [ "$DISABLE_MOSQUITTO" = true ] || fail 'runtime DISABLE_MOSQUITTO was overridden'
  [ "$MQTT_BROKER" = mqtts://runtime-broker.example:8883 ] || fail 'external runtime MQTT_BROKER changed while bundled broker was disabled'
  [ "$DISABLE_CADDY" = true ] || fail 'runtime DISABLE_CADDY was overridden'
  [ "$ENABLE_PPROF" = true ] || fail 'runtime ENABLE_PPROF was overridden'
  [ "$PPROF_HOST" = "" ] || fail 'empty runtime PPROF_HOST was overridden'
  [ "$SERVER_PPROF_PORT" = 6060 ] || fail 'runtime SERVER_PPROF_PORT was overridden'
  [ "$INGESTOR_PPROF_PORT" = 6061 ] || fail 'runtime INGESTOR_PPROF_PORT was overridden'
  [ "$DATA_ONLY" = loaded ] || fail 'non-critical data env value was not loaded'
)

(
  unset DISABLE_MOSQUITTO MQTT_BROKER DISABLE_CADDY ENABLE_PPROF PPROF_HOST SERVER_PPROF_PORT INGESTOR_PPROF_PORT

  . "$HELPER"
  source_data_env_preserving_runtime "$env_file"
  enforce_bundled_mqtt_broker

  [ "$DISABLE_MOSQUITTO" = false ] || fail 'missing DISABLE_MOSQUITTO was not loaded'
  [ "$MQTT_BROKER" = mqtt://localhost:1883 ] || fail 'stale data-volume MQTT_BROKER was not replaced for bundled broker'
  [ "$DISABLE_CADDY" = false ] || fail 'missing DISABLE_CADDY was not loaded'
  [ "$ENABLE_PPROF" = false ] || fail 'missing ENABLE_PPROF was not loaded'
  [ "$PPROF_HOST" = 127.0.0.1 ] || fail 'missing PPROF_HOST was not loaded'
  [ "$SERVER_PPROF_PORT" = 16060 ] || fail 'missing SERVER_PPROF_PORT was not loaded'
  [ "$INGESTOR_PPROF_PORT" = 16061 ] || fail 'missing INGESTOR_PPROF_PORT was not loaded'
)

(
  DISABLE_MOSQUITTO=false
  MQTT_BROKER=mqtts://stale-runtime.example:8883
  export DISABLE_MOSQUITTO MQTT_BROKER

  . "$HELPER"
  source_data_env_preserving_runtime "$env_file"
  enforce_bundled_mqtt_broker

  [ "$MQTT_BROKER" = mqtt://localhost:1883 ] || fail 'stale runtime MQTT_BROKER was not replaced for bundled broker'
)

for false_value in FALSE False 0 no; do
  (
    DISABLE_MOSQUITTO=$false_value
    MQTT_BROKER=mqtts://stale-runtime.example:8883
    export DISABLE_MOSQUITTO MQTT_BROKER

    . "$HELPER"
    enforce_bundled_mqtt_broker

    [ "$MQTT_BROKER" = mqtt://localhost:1883 ] || fail "$false_value did not select and enforce the bundled broker"
  )
done

printf 'PASS: entrypoint runtime env precedence\n'
