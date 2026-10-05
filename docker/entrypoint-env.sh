#!/bin/sh

is_true() {
  case "${1:-}" in
    1|true|TRUE|yes|YES|y|Y|on|ON) return 0 ;;
    *) return 1 ;;
  esac
}

enforce_bundled_mqtt_broker() {
  if ! is_true "${DISABLE_MOSQUITTO:-true}"; then
    MQTT_BROKER=mqtt://localhost:1883
    export MQTT_BROKER
  fi
}

source_data_env_preserving_runtime() {
  _corescope_env_file=$1
  [ -f "$_corescope_env_file" ] || return 0

  _corescope_disable_mosquitto_set=${DISABLE_MOSQUITTO+x}
  _corescope_disable_mosquitto_value=${DISABLE_MOSQUITTO-}
  _corescope_mqtt_broker_set=${MQTT_BROKER+x}
  _corescope_mqtt_broker_value=${MQTT_BROKER-}
  _corescope_disable_caddy_set=${DISABLE_CADDY+x}
  _corescope_disable_caddy_value=${DISABLE_CADDY-}
  _corescope_enable_pprof_set=${ENABLE_PPROF+x}
  _corescope_enable_pprof_value=${ENABLE_PPROF-}
  _corescope_pprof_host_set=${PPROF_HOST+x}
  _corescope_pprof_host_value=${PPROF_HOST-}
  _corescope_server_pprof_port_set=${SERVER_PPROF_PORT+x}
  _corescope_server_pprof_port_value=${SERVER_PPROF_PORT-}
  _corescope_ingestor_pprof_port_set=${INGESTOR_PPROF_PORT+x}
  _corescope_ingestor_pprof_port_value=${INGESTOR_PPROF_PORT-}

  set -a
  . "$_corescope_env_file"
  set +a

  if [ "$_corescope_disable_mosquitto_set" = x ]; then
    DISABLE_MOSQUITTO=$_corescope_disable_mosquitto_value
    export DISABLE_MOSQUITTO
  fi
  if [ "$_corescope_mqtt_broker_set" = x ]; then
    MQTT_BROKER=$_corescope_mqtt_broker_value
    export MQTT_BROKER
  fi
  if [ "$_corescope_disable_caddy_set" = x ]; then
    DISABLE_CADDY=$_corescope_disable_caddy_value
    export DISABLE_CADDY
  fi
  if [ "$_corescope_enable_pprof_set" = x ]; then
    ENABLE_PPROF=$_corescope_enable_pprof_value
    export ENABLE_PPROF
  fi
  if [ "$_corescope_pprof_host_set" = x ]; then
    PPROF_HOST=$_corescope_pprof_host_value
    export PPROF_HOST
  fi
  if [ "$_corescope_server_pprof_port_set" = x ]; then
    SERVER_PPROF_PORT=$_corescope_server_pprof_port_value
    export SERVER_PPROF_PORT
  fi
  if [ "$_corescope_ingestor_pprof_port_set" = x ]; then
    INGESTOR_PPROF_PORT=$_corescope_ingestor_pprof_port_value
    export INGESTOR_PPROF_PORT
  fi

  unset _corescope_env_file \
    _corescope_disable_mosquitto_set _corescope_disable_mosquitto_value \
    _corescope_mqtt_broker_set _corescope_mqtt_broker_value \
    _corescope_disable_caddy_set _corescope_disable_caddy_value \
    _corescope_enable_pprof_set _corescope_enable_pprof_value \
    _corescope_pprof_host_set _corescope_pprof_host_value \
    _corescope_server_pprof_port_set _corescope_server_pprof_port_value \
    _corescope_ingestor_pprof_port_set _corescope_ingestor_pprof_port_value
}
