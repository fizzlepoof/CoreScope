#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

assert_success() {
  local description=$1
  shift
  if ! "$@"; then
    fail "$description"
  fi
}

assert_failure() {
  local description=$1
  shift
  if "$@"; then
    fail "$description"
  fi
}

make_fake_docker() {
  local bin_dir=$1
  cat >"$bin_dir/docker" <<'SH'
#!/bin/sh
printf '%s\n' "$*" >>"$DOCKER_LOG"
case "$*" in
  "compose version") exit 0 ;;
  "ps --format {{.Names}}") printf '%s\n' corescope-prod; exit 0 ;;
  "--version") printf '%s\n' 'Docker version test'; exit 0 ;;
  inspect*Health.Status*) printf '%s\n' healthy; exit 0 ;;
  "exec corescope-prod wget -qO- http://localhost:3000/api/stats") printf '%s\n' '{"totalPackets":42,"totalNodes":7}'; exit 0 ;;
  "exec corescope-prod mosquitto_sub"*) printf '%s\n' sample-message; exit 0 ;;
esac
exit 0
SH
  chmod +x "$bin_dir/docker"
}

run_manage_start() {
  local fixture=$1
  shift
  (
    cd "$fixture"
    env PATH="$fixture/bin:$PATH" \
      HOME="$fixture/home" \
      PROD_DATA_DIR="$fixture/data" \
      DOCKER_LOG="$fixture/docker.log" \
      "$@" \
      bash "$REPO_ROOT/manage.sh" start
  )
}

run_manage() {
  local fixture=$1
  shift
  (
    cd "$fixture"
    env PATH="$fixture/bin:$PATH" \
      HOME="$fixture/home" \
      PROD_DATA_DIR="$fixture/data" \
      DOCKER_LOG="$fixture/docker.log" \
      GIT_LOG="$fixture/git.log" \
      bash "$REPO_ROOT/manage.sh" "$@"
  )
}

make_fake_git() {
  local bin_dir=$1
  cat >"$bin_dir/git" <<'SH'
#!/bin/sh
printf '%s\n' "$*" >>"$GIT_LOG"
case "$1" in
  describe) printf '%s\n' v-test ;;
  rev-parse) printf '%s\n' test123 ;;
  tag) printf '%s\n' v-test ;;
esac
exit 0
SH
  chmod +x "$bin_dir/git"
}

# Library mode must expose helpers without dispatching the CLI or requiring
# callers to fake positional arguments.
source_output=$(CORESCOPE_MANAGE_LIBRARY_MODE=1 bash -c 'source "$1"; declare -F config_is_valid_json >/dev/null; declare -F config_has_external_mqtt_broker >/dev/null; declare -F write_env_managed_values >/dev/null' _ "$REPO_ROOT/manage.sh" 2>&1) || {
  printf '%s\n' "$source_output" >&2
  fail "manage.sh could not be sourced in library mode"
}
[[ -z $source_output ]] || fail "library mode produced CLI output: $source_output"

CORESCOPE_MANAGE_LIBRARY_MODE=1 source "$REPO_ROOT/manage.sh"

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT

valid_json_corpus=(
  '{}'
  '{"mqttSources":[]}'
  '{"mqtt":{"broker":"mqtts://broker.example:8883"}}'
)
invalid_json_corpus=(
  '{"mqttSources":['
  '{"mqttSources":NaN}'
  '{} {}'
)
for index in "${!valid_json_corpus[@]}"; do
  config="$tmp_dir/valid-json-$index.json"
  printf '%s\n' "${valid_json_corpus[$index]}" >"$config"
  assert_success "valid JSON corpus entry $index should be accepted" config_is_valid_json "$config"
done
for index in "${!invalid_json_corpus[@]}"; do
  config="$tmp_dir/invalid-json-$index.json"
  printf '%s\n' "${invalid_json_corpus[$index]}" >"$config"
  assert_failure "invalid JSON corpus entry $index should be rejected" config_is_valid_json "$config"
done
mkdir "$tmp_dir/no-json-parsers"
if (PATH="$tmp_dir/no-json-parsers"; config_is_valid_json "$tmp_dir/valid-json-0.json"); then
  fail "JSON validation should fail closed without a parser"
fi

make_config() {
  local destination=$1
  local broker=$2
  cat >"$destination" <<JSON
{"mqttSources":[{"name":"test","broker":"$broker","username":"operator","password":"secret"}]}
JSON
}

for scheme in mqtt mqtts ws wss tcp ssl; do
  config="$tmp_dir/$scheme.json"
  make_config "$config" "$scheme://broker.example:1883"
  assert_success "remote $scheme broker should be accepted" config_has_external_mqtt_broker "$config"
done

for broker in \
  mqtt://localhost:1883 \
  mqtt://localhost.:1883 \
  mqtts://127.0.0.1:8883 \
  ws://localhost:9001/mqtt \
  wss://127.0.0.1:9443/mqtt \
  tcp://localhost:1883 \
  ssl://127.0.0.1:8883 \
  'mqtt://[::1]:1883' \
  'mqtt://[::1%lo]:1883' \
  'mqtt://[0:0:0:0:0:0:0:1]:1883' \
  'mqtt://[::ffff:127.0.0.1]:1883' \
  'mqtt://[::ffff:7f00:2]:1883' \
  'mqtt://[0:0:0:0:0:ffff:7f00:2]:1883' \
  'mqtt://[::ffff:7fff:ffff]:1883'; do
  safe_name=${broker//[^a-zA-Z0-9]/_}
  config="$tmp_dir/$safe_name.json"
  make_config "$config" "$broker"
  assert_failure "local broker $broker should be rejected" config_has_external_mqtt_broker "$config"
done

remote_ipv6_config="$tmp_dir/remote-ipv6.json"
make_config "$remote_ipv6_config" 'mqtt://[2001:db8::42]:1883'
assert_success "remote IPv6 broker should be accepted" config_has_external_mqtt_broker "$remote_ipv6_config"

secret_broker='mqtts://alice:super-secret@broker.example:8883/mqtt?auth=unknown-secret&X-Amz-Credential=aws-credential&X-Amz-Signature=aws-signature&client=visible#fragment-secret'
sanitized_broker=$(sanitize_broker_url_for_display "$secret_broker")
[[ $sanitized_broker == 'mqtts://broker.example:8883/mqtt' ]] || fail "broker display sanitizer returned: $sanitized_broker"
[[ $sanitized_broker != *alice* && $sanitized_broker != *super-secret* && $sanitized_broker != *unknown-secret* && $sanitized_broker != *aws-credential* && $sanitized_broker != *aws-signature* && $sanitized_broker != *visible* && $sanitized_broker != *fragment-secret* ]] || fail "broker display sanitizer leaked credentials"

redaction_output=$(show_env_port_summary 80 443 1883 /srv/corescope true "$secret_broker")
[[ $redaction_output == *"$sanitized_broker"* ]] || fail "environment summary did not display the sanitized broker"
[[ $redaction_output != *super-secret* && $redaction_output != *unknown-secret* && $redaction_output != *aws-credential* && $redaction_output != *aws-signature* && $redaction_output != *fragment-secret* ]] || fail "environment summary leaked broker credentials"

prompt_display="$tmp_dir/broker-prompt"
prompt_return="$tmp_dir/broker-return"
(
  read() {
    printf '%s' "$3" >"$prompt_display"
    printf -v "$4" '%s' ''
  }
  prompt_for_external_mqtt_broker "$secret_broker" >"$prompt_return"
)
[[ $(<"$prompt_return") == "$secret_broker" ]] || fail "broker prompt changed the operational URL"
grep -Fq "$sanitized_broker" "$prompt_display" || fail "broker prompt did not display the sanitized URL"
if grep -Eq 'super-secret|unknown-secret|aws-credential|aws-signature|fragment-secret' "$prompt_display"; then
  fail "broker prompt leaked credentials"
fi

(
  cd "$tmp_dir"
  printf 'UNRELATED=preserved\nMQTT_BROKER=old-value\n' >.env
  write_env_managed_values 8080 8443 1883 /srv/corescope true mqtts://broker.example:8883
  [[ $(get_env_value MQTT_BROKER .env) == mqtts://broker.example:8883 ]] || fail "external MQTT_BROKER was not persisted when bundled broker was disabled"
  [[ $(get_env_value UNRELATED .env) == preserved ]] || fail "unmanaged env value was not preserved"

  write_env_managed_values 8080 8443 1883 /srv/corescope false mqtts://broker.example:8883
  [[ -z $(get_env_value MQTT_BROKER .env) ]] || fail "MQTT_BROKER was not cleared when bundled broker was enabled"

  write_env_managed_values 8080 8443 1883 /srv/corescope true "$secret_broker"
  [[ $(get_env_value MQTT_BROKER .env) == "$secret_broker" ]] || fail "display sanitization changed the persisted operational URL"
)

blocked_start="$tmp_dir/blocked-start"
mkdir -p "$blocked_start/bin" "$blocked_start/data" "$blocked_start/home"
make_fake_docker "$blocked_start/bin"
make_config "$blocked_start/data/config.json" "mqtt://localhost:1883"
printf 'DISABLE_MOSQUITTO=true\nMQTT_BROKER=\n' >"$blocked_start/.env"
if run_manage_start "$blocked_start" >"$blocked_start/output" 2>&1; then
  fail "start launched without an external broker while bundled Mosquitto was disabled"
fi
if grep -q 'up -d prod' "$blocked_start/docker.log"; then
  fail "start reached docker compose despite missing an external broker"
fi
if ! grep -q 'external MQTT broker' "$blocked_start/output"; then
  fail "start failure did not explain the external broker requirement"
fi

external_config_start="$tmp_dir/external-config-start"
mkdir -p "$external_config_start/bin" "$external_config_start/data" "$external_config_start/home"
make_fake_docker "$external_config_start/bin"
make_config "$external_config_start/data/config.json" "mqtts://broker.example:8883"
printf 'DISABLE_MOSQUITTO=true\nMQTT_BROKER=\n' >"$external_config_start/.env"
assert_success "start should preserve an existing external config source" run_manage_start "$external_config_start"
grep -q 'compose -f docker-compose.no-mosquitto.yml up -d prod' "$external_config_start/docker.log" || fail "start did not launch with existing external config"

external_env_start="$tmp_dir/external-env-start"
mkdir -p "$external_env_start/bin" "$external_env_start/data" "$external_env_start/home"
make_fake_docker "$external_env_start/bin"
make_config "$external_env_start/data/config.json" "mqtt://localhost:1883"
printf 'DISABLE_MOSQUITTO=true\nMQTT_BROKER=mqtts://broker.example:8883\n' >"$external_env_start/.env"
assert_success "start should accept an external broker from .env" run_manage_start "$external_env_start"
grep -q 'compose -f docker-compose.no-mosquitto.yml up -d prod' "$external_env_start/docker.log" || fail "start did not launch with external .env broker"

bundled_start="$tmp_dir/bundled-start"
mkdir -p "$bundled_start/bin" "$bundled_start/data" "$bundled_start/home"
make_fake_docker "$bundled_start/bin"
make_config "$bundled_start/data/config.json" "mqtt://localhost:1883"
printf 'DISABLE_MOSQUITTO=false\nMQTT_BROKER=\n' >"$bundled_start/.env"
assert_success "start should preserve explicit bundled-broker users" run_manage_start "$bundled_start"
grep -q 'compose up -d prod' "$bundled_start/docker.log" || fail "start did not launch the explicit bundled-broker compose"

make_lifecycle_fixture() {
  local fixture=$1
  local disable_mosquitto=$2
  local broker=$3
  mkdir -p "$fixture/bin" "$fixture/data" "$fixture/home" "$fixture/backup"
  make_fake_docker "$fixture/bin"
  make_fake_git "$fixture/bin"
  make_config "$fixture/data/config.json" "$broker"
  make_config "$fixture/backup/config.json" "$broker"
  : >"$fixture/data/meshcore.db"
  : >"$fixture/backup/meshcore.db"
  printf 'DISABLE_MOSQUITTO=%s\nMQTT_BROKER=\n' "$disable_mosquitto" >"$fixture/.env"
}

assert_no_prod_launch() {
  local fixture=$1
  if grep -Eq 'compose .*up .*prod|compose up .*prod' "$fixture/docker.log"; then
    fail "production launch reached compose for $fixture"
  fi
}

run_lifecycle_command() {
  local fixture=$1
  local command=$2
  case "$command" in
    restart-prod) run_manage "$fixture" restart prod ;;
    restart-all) run_manage "$fixture" restart all ;;
    update) run_manage "$fixture" update v-test ;;
    promote) printf 'y' | run_manage "$fixture" promote ;;
    restore) printf 'y' | run_manage "$fixture" restore "$fixture/backup" ;;
  esac
}

for command in restart-prod restart-all update promote restore; do
  blocked="$tmp_dir/blocked-$command"
  make_lifecycle_fixture "$blocked" true mqtt://localhost:1883
  if run_lifecycle_command "$blocked" "$command" >"$blocked/output" 2>&1; then
    fail "$command launched without an external broker while bundled Mosquitto was disabled"
  fi
  assert_no_prod_launch "$blocked"
  grep -q 'external MQTT broker' "$blocked/output" || fail "$command failure did not explain the external broker requirement"

  external="$tmp_dir/external-$command"
  make_lifecycle_fixture "$external" true mqtts://broker.example:8883
  assert_success "$command should preserve external broker deployments" run_lifecycle_command "$external" "$command"
  grep -Eq 'compose -f docker-compose.no-mosquitto.yml .*up .*prod' "$external/docker.log" || fail "$command did not launch the external-broker compose"

  bundled="$tmp_dir/bundled-$command"
  make_lifecycle_fixture "$bundled" false mqtt://localhost:1883
  assert_success "$command should preserve bundled broker deployments" run_lifecycle_command "$bundled" "$command"
  grep -Eq 'compose .*up .*prod|compose up .*prod' "$bundled/docker.log" || fail "$command did not launch the bundled-broker compose"
done

invalid_restore="$tmp_dir/invalid-restore"
make_lifecycle_fixture "$invalid_restore" true mqtts://current.example:8883
make_config "$invalid_restore/backup/config.json" mqtt://localhost:1883
printf 'original-db\n' >"$invalid_restore/data/meshcore.db"
printf 'restored-db\n' >"$invalid_restore/backup/meshcore.db"
original_config=$(<"$invalid_restore/data/config.json")
if printf 'y' | run_manage "$invalid_restore" restore "$invalid_restore/backup" >"$invalid_restore/output" 2>&1; then
  fail "restore accepted a candidate config without an external broker"
fi
[[ $(<"$invalid_restore/data/meshcore.db") == original-db ]] || fail "rejected restore replaced the original database"
[[ $(<"$invalid_restore/data/config.json") == "$original_config" ]] || fail "rejected restore replaced the original config"
if grep -Eq 'compose .*(stop|down|up).*prod|compose (stop|down|up).*prod' "$invalid_restore/docker.log"; then
  fail "rejected restore stopped or relaunched the running service"
fi
if compgen -G "$invalid_restore/backups/corescope-pre-restore-*" >/dev/null; then
  fail "rejected restore created a pre-restore backup before validation"
fi

malformed_restore="$tmp_dir/malformed-restore"
make_lifecycle_fixture "$malformed_restore" true mqtts://current.example:8883
printf 'DISABLE_MOSQUITTO=true\nMQTT_BROKER=mqtts://env.example:8883\n' >"$malformed_restore/.env"
printf '{"mqttSources":[' >"$malformed_restore/backup/config.json"
printf 'original-db\n' >"$malformed_restore/data/meshcore.db"
printf 'restored-db\n' >"$malformed_restore/backup/meshcore.db"
malformed_restore_config=$(<"$malformed_restore/data/config.json")
mkdir -p "$malformed_restore/caddy-config"
printf 'original-caddy\n' >"$malformed_restore/caddy-config/Caddyfile"
printf 'replacement-caddy\n' >"$malformed_restore/backup/Caddyfile"
printf 'original-theme\n' >"$malformed_restore/data/theme.json"
printf 'replacement-theme\n' >"$malformed_restore/backup/theme.json"
if printf 'y' | run_manage "$malformed_restore" restore "$malformed_restore/backup" >"$malformed_restore/output" 2>&1; then
  fail "restore accepted malformed candidate config with a valid external MQTT_BROKER"
fi
[[ $(<"$malformed_restore/data/meshcore.db") == original-db ]] || fail "malformed restore replaced the original database"
[[ $(<"$malformed_restore/data/config.json") == "$malformed_restore_config" ]] || fail "malformed restore replaced the original config"
[[ $(<"$malformed_restore/caddy-config/Caddyfile") == original-caddy ]] || fail "malformed restore replaced the original Caddyfile"
[[ $(<"$malformed_restore/data/theme.json") == original-theme ]] || fail "malformed restore replaced the original theme"
if grep -q 'Continue?' "$malformed_restore/output"; then
  fail "malformed restore prompted for confirmation before rejecting the candidate"
fi
if grep -Eq 'compose .*(stop|down|up).*prod|compose (stop|down|up).*prod' "$malformed_restore/docker.log"; then
  fail "malformed restore stopped or relaunched the running service"
fi
if compgen -G "$malformed_restore/backups/corescope-pre-restore-*" >/dev/null; then
  fail "malformed restore created a pre-restore backup before validation"
fi
grep -q 'valid JSON' "$malformed_restore/output" || fail "malformed restore failure did not explain the JSON requirement"

malformed_start="$tmp_dir/malformed-start"
mkdir -p "$malformed_start/bin" "$malformed_start/data" "$malformed_start/home"
make_fake_docker "$malformed_start/bin"
printf '{"mqttSources":[' >"$malformed_start/data/config.json"
printf 'DISABLE_MOSQUITTO=true\nMQTT_BROKER=mqtts://broker.example:8883\n' >"$malformed_start/.env"
malformed_start_config=$(<"$malformed_start/data/config.json")
if run_manage_start "$malformed_start" >"$malformed_start/output" 2>&1; then
  fail "start accepted malformed active config with a valid external MQTT_BROKER"
fi
[[ $(<"$malformed_start/data/config.json") == "$malformed_start_config" ]] || fail "rejected start mutated the malformed active config"
if grep -Eq 'compose .*up .*prod|compose up .*prod' "$malformed_start/docker.log"; then
  fail "malformed active config reached docker compose up"
fi
grep -q 'valid JSON' "$malformed_start/output" || fail "malformed active config failure did not explain the JSON requirement"

external_env_restore="$tmp_dir/external-env-restore"
make_lifecycle_fixture "$external_env_restore" true mqtt://localhost:1883
printf 'DISABLE_MOSQUITTO=true\nMQTT_BROKER=mqtts://env.example:8883\n' >"$external_env_restore/.env"
assert_success "valid external MQTT_BROKER should override a local candidate restore config" run_lifecycle_command "$external_env_restore" restore
grep -Eq 'compose -f docker-compose.no-mosquitto.yml .*up .*prod' "$external_env_restore/docker.log" || fail "external-env restore did not relaunch production"

external_config_restore="$tmp_dir/external-config-restore"
make_lifecycle_fixture "$external_config_restore" true mqtt://localhost:1883
make_config "$external_config_restore/backup/config.json" mqtts://candidate.example:8883
assert_success "valid candidate external config should restore" run_lifecycle_command "$external_config_restore" restore
grep -Eq 'compose -f docker-compose.no-mosquitto.yml .*up .*prod' "$external_config_restore/docker.log" || fail "external-config restore did not relaunch production"

bundled_restore="$tmp_dir/explicit-bundled-restore"
make_lifecycle_fixture "$bundled_restore" false mqtt://localhost:1883
assert_success "explicit bundled mode should allow a local candidate restore config" run_lifecycle_command "$bundled_restore" restore
grep -Eq 'compose .*up .*prod|compose up .*prod' "$bundled_restore/docker.log" || fail "bundled restore did not relaunch production"

status_redaction="$tmp_dir/status-redaction"
make_lifecycle_fixture "$status_redaction" true mqtts://broker.example:8883
printf 'DISABLE_MOSQUITTO=true\nMQTT_BROKER=%s\n' "$secret_broker" >"$status_redaction/.env"
run_manage "$status_redaction" status >"$status_redaction/output" 2>&1
grep -Fq "$sanitized_broker" "$status_redaction/output" || fail "status did not display the sanitized broker"
if grep -Eq 'super-secret|unknown-secret|aws-credential|aws-signature|fragment-secret' "$status_redaction/output"; then
  fail "status leaked broker credentials"
fi

external_mqtt_test="$tmp_dir/external-mqtt-test"
make_lifecycle_fixture "$external_mqtt_test" true mqtts://broker.example:8883
run_manage "$external_mqtt_test" mqtt-test >"$external_mqtt_test/output" 2>&1
grep -q '42 packets, 7 nodes' "$external_mqtt_test/output" || fail "external mqtt-test did not report API ingestion status"
if grep -q mosquitto_sub "$external_mqtt_test/docker.log"; then
  fail "external mqtt-test attempted to subscribe to the absent bundled broker"
fi

bundled_mqtt_test="$tmp_dir/bundled-mqtt-test"
make_lifecycle_fixture "$bundled_mqtt_test" false mqtt://localhost:1883
run_manage "$bundled_mqtt_test" mqtt-test >"$bundled_mqtt_test/output" 2>&1
grep -q 'Received MQTT message' "$bundled_mqtt_test/output" || fail "bundled mqtt-test no longer checks the local broker"
grep -q mosquitto_sub "$bundled_mqtt_test/docker.log" || fail "bundled mqtt-test did not subscribe to the local broker"

noninteractive_setup="$tmp_dir/noninteractive-setup"
mkdir -p "$noninteractive_setup/bin" "$noninteractive_setup/data" "$noninteractive_setup/home" "$noninteractive_setup/caddy-config"
make_fake_docker "$noninteractive_setup/bin"
make_config "$noninteractive_setup/data/config.json" "mqtt://localhost:1883"
printf 'DISABLE_MOSQUITTO=true\nMQTT_BROKER=\nPROD_HTTP_PORT=8080\nPROD_HTTPS_PORT=8443\nPROD_MQTT_PORT=1883\nPROD_DATA_DIR=%s\n' "$noninteractive_setup/data" >"$noninteractive_setup/.env"
printf ':8080 {\n    reverse_proxy localhost:3000\n}\n' >"$noninteractive_setup/caddy-config/Caddyfile"
set +e
printf 'y' | timeout 3s env \
  PATH="$noninteractive_setup/bin:$PATH" \
  HOME="$noninteractive_setup/home" \
  DOCKER_LOG="$noninteractive_setup/docker.log" \
  bash "$REPO_ROOT/manage.sh" setup >"$noninteractive_setup/output" 2>&1
setup_rc=$?
set -e
[[ $setup_rc -ne 0 ]] || fail "noninteractive setup accepted a missing external broker"
[[ $setup_rc -ne 124 ]] || fail "noninteractive setup hung prompting for an external broker"
if grep -q 'up -d prod' "$noninteractive_setup/docker.log"; then
  fail "noninteractive setup reached docker compose without an external broker"
fi
if ! grep -q 'external MQTT broker' "$noninteractive_setup/output"; then
  fail "noninteractive setup failure did not explain the external broker requirement"
fi

printf 'PASS: manage.sh runtime behavior\n'
