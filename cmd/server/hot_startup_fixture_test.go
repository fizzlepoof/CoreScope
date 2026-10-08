package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Check the actual fallback SELECT, not merely index names: correlated
// observation lookups must use the same indexed boundary as the ingestor DB.
func TestHotStartupFixture_SQLFallbackUsesIndexedObservationLookups(t *testing.T) {
	dbPath := createTestDBMultiDay(t, 5, 200)
	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.conn.Close()
	q := PacketQuery{Since: time.Now().UTC().Add(-200 * time.Hour).Format(time.RFC3339), Limit: 5000, Order: "ASC"}
	cols, join := db.transmissionBaseSQL()
	where, args := db.buildTransmissionWhere(q)
	query := fmt.Sprintf("SELECT %s FROM transmissions t %s WHERE %s ORDER BY t.id ASC LIMIT ? OFFSET ?", cols, join, strings.Join(where, " AND "))
	args = append(args, q.Limit, q.Offset)
	rows, err := db.conn.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	indexedLookups := 0
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		t.Log(detail)
		if strings.Contains(detail, "SCAN observations") {
			t.Errorf("hot-startup fixture forces full observation scan: %s", detail)
		}
		if strings.Contains(detail, "SEARCH observations") && strings.Contains(detail, "transmission_id=?") {
			indexedLookups++
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if indexedLookups != 2 {
		t.Errorf("expected both correlated observation lookups indexed, got %d", indexedLookups)
	}
}
