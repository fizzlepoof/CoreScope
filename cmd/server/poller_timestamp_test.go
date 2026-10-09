package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const fallbackFirstSeen = "2026-01-16T10:00:00Z"

func TestGetNewTransmissionsSinceTimestampAlias(t *testing.T) {
	// A nullable legacy schema also exercises the existing nullStr semantics.
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	if _, err := conn.Exec(`CREATE TABLE transmissions (
		id INTEGER PRIMARY KEY, raw_hex TEXT, hash TEXT, first_seen TEXT,
		route_type INTEGER, payload_type INTEGER, payload_version INTEGER, decoded_json TEXT)`); err != nil {
		t.Fatal(err)
	}
	db := &DB{conn: conn}
	for i, firstSeen := range []interface{}{fallbackFirstSeen, "2026-01-16 10:00:00", nil} {
		if _, err := conn.Exec(`INSERT INTO transmissions VALUES (?, 'EEFF', ?, ?, 1, 4, 0, '{}')`, i+1, fmt.Sprintf("fallback-%d", i+1), firstSeen); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := db.GetNewTransmissionsSince(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d transmissions, want 3", len(rows))
	}
	for i, want := range []interface{}{fallbackFirstSeen, "2026-01-16 10:00:00", nil} {
		row := rows[i]
		if row["id"] != i+1 || row["hash"] != fmt.Sprintf("fallback-%d", i+1) || row["payload_type"] != 4 || row["raw_hex"] != "EEFF" || row["decoded_json"] != "{}" {
			t.Fatalf("unexpected transmission fields: %#v", row)
		}
		if row["first_seen"] != want {
			t.Errorf("first_seen = %#v, want %#v", row["first_seen"], want)
		}
		got, exists := row["timestamp"]
		if !exists || got != want {
			t.Errorf("transmission %d timestamp alias = %#v (present=%v), want %#v", i+1, got, exists, want)
		}
	}
	if _, err := time.Parse(time.RFC3339, rows[0]["first_seen"].(string)); err != nil {
		t.Fatal(err)
	}
	page, err := db.GetNewTransmissionsSince(1, 1)
	if err != nil || len(page) != 1 || page[0]["id"] != 2 {
		t.Fatalf("cursor/limit changed: page=%#v err=%v", page, err)
	}
}

// Insert bounded, distinct real rows until the first delivery, not until a
// conforming delivery. Poller.Start snapshots max ID without a readiness signal;
// a later row makes this independent of which first insert startup skips.
func insertFallbackTransmission(t *testing.T, db *DB, sequence int) {
	t.Helper()
	if _, err := db.conn.Exec(`INSERT INTO transmissions (raw_hex, hash, first_seen, route_type, payload_type)
		VALUES ('EEFF', ?, ?, 1, 4)`, fmt.Sprintf("fallback-live-%d", sequence), fallbackFirstSeen); err != nil {
		t.Fatalf("insert fallback transmission: %v", err)
	}
}

func assertFallbackPacketMessage(t *testing.T, db *DB, msg []byte) {
	t.Helper()
	if len(msg) == 0 {
		t.Fatal("expected non-empty broadcast message")
	}
	var parsed struct {
		Type string `json:"type"`
		Data struct {
			Timestamp string `json:"timestamp"`
			FirstSeen string `json:"first_seen"`
			Packet    struct {
				ID          int    `json:"id"`
				Hash        string `json:"hash"`
				PayloadType int    `json:"payload_type"`
				Timestamp   string `json:"timestamp"`
				FirstSeen   string `json:"first_seen"`
			} `json:"packet"`
		} `json:"data"`
	}
	if err := json.Unmarshal(msg, &parsed); err != nil {
		t.Fatalf("parse actual Hub broadcast: %v", err)
	}
	pkt := parsed.Data.Packet
	if parsed.Type != "packet" || pkt.ID <= 0 || pkt.Hash == "" || pkt.PayloadType != 4 {
		t.Fatalf("unexpected packet message: %s", msg)
	}
	if pkt.Timestamp != fallbackFirstSeen || pkt.FirstSeen != fallbackFirstSeen || parsed.Data.Timestamp != fallbackFirstSeen || parsed.Data.FirstSeen != fallbackFirstSeen {
		t.Fatalf("expected exact timestamp/first_seen %q at data and data.packet: %s", fallbackFirstSeen, msg)
	}
	if _, err := time.Parse(time.RFC3339, pkt.Timestamp); err != nil {
		t.Fatalf("unparseable data.packet.timestamp: %v", err)
	}
	var hash, firstSeen string
	var payloadType int
	if err := db.conn.QueryRow(`SELECT hash, first_seen, payload_type FROM transmissions WHERE id = ?`, pkt.ID).Scan(&hash, &firstSeen, &payloadType); err != nil {
		t.Fatal(err)
	}
	if hash != pkt.Hash || firstSeen != pkt.Timestamp || payloadType != pkt.PayloadType {
		t.Fatal("delivered packet does not match its persisted SQLite row")
	}
}

func TestPollerNilStoreTimestampWebSocket(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	hub := NewHub()
	srv := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer srv.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+srv.URL[4:], nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	waitForClientCount(t, hub, 1)
	poller := NewPoller(db, hub, 50*time.Millisecond)
	done := make(chan struct{})
	go func() { defer close(done); poller.Start() }()
	defer func() { poller.Stop(); <-done }()
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	type delivery struct {
		msg []byte
		err error
	}
	received := make(chan delivery, 1)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		_, msg, err := conn.ReadMessage()
		received <- delivery{msg, err}
	}()
	defer func() { conn.Close(); <-readerDone }()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	insertFallbackTransmission(t, db, 0)
	for sequence := 1; ; {
		select {
		case result := <-received:
			if result.err != nil {
				t.Fatalf("actual WebSocket first delivery: %v", result.err)
			}
			assertFallbackPacketMessage(t, db, result.msg)
			return
		case <-ticker.C:
			insertFallbackTransmission(t, db, sequence)
			sequence++
		case <-deadline.C:
			t.Fatal("no actual nil-store WebSocket message within 2 seconds")
		}
	}
}
