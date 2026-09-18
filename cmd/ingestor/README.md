# MeshCore MQTT Ingestor (Go)

Standalone MQTT ingestion service for CoreScope. It connects to MQTT brokers,
decodes raw MeshCore packets, and owns all writes to the SQLite database shared
with the Go API server.

## Architecture

```
MQTT Broker(s)  →  Go Ingestor  →  SQLite DB  ←  Go Server
                    (this binary)     (server is read-only)
```

- **Single static binary** — no runtime dependencies, no CGO
- **SQLite** via `modernc.org/sqlite` (pure Go)
- **MQTT** via `github.com/eclipse/paho.mqtt.golang`
- Runs alongside the Go server; both processes share the DB file
- Does not serve HTTP/WebSocket; the Go server owns those interfaces

## Build

Requires Go 1.22+.

```bash
cd cmd/ingestor
go build -o corescope-ingestor .
```

Cross-compile for Linux (e.g., for the production VM):

```bash
GOOS=linux GOARCH=amd64 go build -o corescope-ingestor .
```

## Run

```bash
./corescope-ingestor -config /path/to/config.json
```

The config file uses CoreScope's `config.json` format. The ingestor reads the
`mqttSources` array (or legacy `mqtt` object) and `dbPath` fields.

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `DB_PATH` | SQLite database path | `data/meshcore.db` |
| `MQTT_BROKER` | Single MQTT broker URL (overrides config) | — |
| `MQTT_TOPIC` | MQTT topic (used with `MQTT_BROKER`) | `meshcore/#` |
| `CORESCOPE_INGESTOR_STATS` | Path to the per-second stats JSON file consumed by the server's `/api/perf/io` and `/api/perf/write-sources` endpoints (#1120) | `/tmp/corescope-ingestor-stats.json` |

### Stats file (`CORESCOPE_INGESTOR_STATS`)

Every second the ingestor publishes a JSON snapshot of its counters
(`tx_inserted`, `obs_inserted`, `walCommits`, `backfillUpdates.*`, etc.) plus
a `procIO` block sampled from `/proc/self/io` (read/write/cancelled bytes per
second + syscall counts). The server reads this file and surfaces the data on
the Perf page so operators can self-diagnose write-volume anomalies.

The writer uses `O_NOFOLLOW | O_CREAT | O_TRUNC` mode `0o600`, so a
pre-planted symlink at the path cannot be used to clobber an arbitrary file.

**Security note:** the default lives in `/tmp`, which is world-writable on
most hosts (sticky bit only protects deletion, not creation). On
shared/multi-tenant hosts, override `CORESCOPE_INGESTOR_STATS` to point at a
private directory (e.g. `/var/lib/corescope/ingestor-stats.json`) that only
the corescope user can write to.

### Minimal Config

```json
{
  "dbPath": "data/meshcore.db",
  "mqttSources": [
    {
      "name": "local",
      "broker": "mqtt://localhost:1883",
      "topics": ["meshcore/#"]
    }
  ]
}
```

### Full Config

The ingestor reads these fields from the existing `config.json`:

- `mqttSources[]` — array of MQTT broker connections
  - `name` — display name for logging
  - `broker` — MQTT URL (`mqtt://`, `mqtts://`)
  - `username` / `password` — auth credentials
  - `topics` — array of topic patterns to subscribe
  - `iataFilter` — optional regional filter
- `mqtt` — legacy single-broker config (auto-converted to `mqttSources`)
- `dbPath` — SQLite DB path (default: `data/meshcore.db`)

## Test

```bash
cd cmd/ingestor
go test -v ./...
```

## What It Does

1. Connects to configured MQTT brokers with auto-reconnect
2. Subscribes to mesh packet topics (e.g., `meshcore/+/+/packets`)
3. Receives raw hex packets via JSON messages (`{ "raw": "...", "SNR": ..., "RSSI": ... }`)
4. Decodes MeshCore packet headers, paths, and payloads (ported from `decoder.js`)
5. Computes content hashes (path-independent, SHA-256-based)
6. Writes to SQLite: `transmissions` + `observations` tables
7. Upserts `nodes` from decoded ADVERT packets (with validation)
8. Upserts `observers` from MQTT topic metadata

## Schema Compatibility

The Go ingestor creates and maintains the v3 schema:

- `transmissions` — deduplicated by content hash
- `observations` — per-observer sightings with `observer_idx` (rowid reference)
- `nodes` — mesh nodes discovered from adverts
- `observers` — MQTT feed sources

All database writes are owned by the ingestor. The server opens SQLite read-only
and polls it for packets to expose through REST and WebSocket APIs.

## Process Responsibilities

- Companion bridge format (Format 2 — `meshcore/advertisement`, channel messages, etc.)
- Channel key decryption (GRP_TXT encrypted payload decryption)
- WebSocket broadcast to browsers is handled by the Go server
- The in-memory packet store and cache invalidation are handled by the Go server

## Files

```
cmd/ingestor/
  main.go          — entry point, MQTT connect, message handler
  decoder.go       — MeshCore packet decoder (ported from decoder.js)
  decoder_test.go  — decoder tests (25 tests, golden fixtures)
  db.go            — SQLite writer (schema-compatible with db.js)
  db_test.go       — DB tests (schema validation, insert/upsert, E2E)
  config.go        — config struct + loader
  util.go          — shared utilities
  go.mod / go.sum  — Go module definition
```
