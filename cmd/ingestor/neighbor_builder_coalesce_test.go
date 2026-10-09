package main

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// Audit actual SQLite row mutations, not Go calls or driver internals.
func neighborCoalesceStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStore(filepath.Join(t.TempDir(), "coalesce.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.WaitForAsyncMigrations()
	neighborCoalesceExec(t, s.db, `INSERT INTO nodes (public_key, name) VALUES ('aaaaaaaaaa', 'a'), ('bbbbbbbbbb', 'b')`)
	neighborCoalesceExec(t, s.db, `INSERT INTO observers (id, name) VALUES ('obs-1', 'observer'), ('aaaaaaaaaa', 'a'), ('bbbbbbbbbb', 'b')`)
	neighborCoalesceExec(t, s.db, `CREATE TABLE neighbor_mutations (kind TEXT, a TEXT, b TEXT, contribution INTEGER, ts TEXT)`)
	neighborCoalesceExec(t, s.db, `CREATE TRIGGER neighbor_audit_insert AFTER INSERT ON neighbor_edges BEGIN
		INSERT INTO neighbor_mutations VALUES ('insert', NEW.node_a, NEW.node_b, NEW.count, NEW.last_seen); END`)
	neighborCoalesceExec(t, s.db, `CREATE TRIGGER neighbor_audit_update AFTER UPDATE ON neighbor_edges BEGIN
		INSERT INTO neighbor_mutations VALUES ('update', NEW.node_a, NEW.node_b, NEW.count - OLD.count, NEW.last_seen); END`)
	return s
}

func neighborCoalesceExec(t *testing.T, db *sql.DB, query string, args ...any) sql.Result {
	t.Helper()
	res, err := db.Exec(query, args...)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func neighborCoalesceObservation(t *testing.T, s *Store, epoch int64, from, path, observer string) {
	t.Helper()
	res := neighborCoalesceExec(t, s.db, `INSERT INTO transmissions
		(raw_hex, hash, first_seen, route_type, payload_type, payload_version, decoded_json, from_pubkey)
		VALUES ('', ?, ?, 0, ?, 0, '{}', ?)`, fmt.Sprintf("%d-%s-%s", epoch, from, observer), epoch, payloadADVERT, from)
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	neighborCoalesceExec(t, s.db, `INSERT INTO observations (transmission_id, observer_idx, path_json, timestamp)
		VALUES (?, (SELECT rowid FROM observers WHERE id = ?), ?, ?)`, id, observer, path, epoch)
}

type neighborCoalesceRow struct {
	a, b  string
	count int
	ts    string
}

func neighborCoalesceRows(t *testing.T, s *Store) []neighborCoalesceRow {
	t.Helper()
	rows, err := s.db.Query(`SELECT node_a, node_b, count, last_seen FROM neighbor_edges ORDER BY node_a, node_b`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []neighborCoalesceRow
	for rows.Next() {
		var row neighborCoalesceRow
		if err := rows.Scan(&row.a, &row.b, &row.count, &row.ts); err != nil {
			t.Fatal(err)
		}
		got = append(got, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return got
}

func neighborCoalesceAuditCount(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM neighbor_mutations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestNeighborEdgesCoalesceBoundedWrites(t *testing.T) {
	s := neighborCoalesceStore(t)
	const start int64 = 1735689600
	for i := int64(0); i < 4; i++ {
		neighborCoalesceObservation(t, s, start+i, "aaaaaaaaaa", `["bb"]`, "obs-1")
	}
	n, err := s.buildAndPersistNeighborEdges()
	if err != nil || n != 8 {
		t.Fatalf("candidate contributions: got %d, %v; want 8, nil", n, err)
	}
	tail := time.Unix(start+3, 0).UTC().Format(time.RFC3339)
	want := []neighborCoalesceRow{{"aaaaaaaaaa", "bbbbbbbbbb", 4, tail}, {"bbbbbbbbbb", "obs-1", 4, tail}}
	if got := neighborCoalesceRows(t, s); !reflect.DeepEqual(got, want) {
		t.Fatalf("persisted contributions/tail: got %+v, want %+v", got, want)
	}
	if got := neighborCoalesceAuditCount(t, s); got != 2 {
		t.Fatalf("bounded SQLite mutations: got %d, want 2 (one per canonical pair)", got)
	}
}
