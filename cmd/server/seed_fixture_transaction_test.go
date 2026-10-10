package main

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// Fixture consumers must see either no seeded rows or the complete dataset,
// not an autocommitted prefix. The independent reader observes real SQLite
// visibility without driver hooks or a scheduler-dependent timing threshold.
func TestSeedTestDBRows_AtomicCommittedFixture(t *testing.T) {
	const numTx, obsPerTx = 3, 2
	const epoch int64 = 1700000000
	path := filepath.Join(t.TempDir(), "fixture.db")
	checked := false
	seedTestDBRows(t, path, numTx, obsPerTx, func(i int) (string, int64) {
		if i == 2 {
			reader, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			for _, table := range []string{"transmissions", "observations"} {
				var count int
				if err := reader.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
					t.Fatal(err)
				}
				if count != 0 {
					t.Errorf("partial fixture visible before seed completes: %s count=%d, want 0", table, count)
				}
			}
			checked = true
		}
		u := epoch + int64(i)*60
		return time.Unix(u, 0).UTC().Format(time.RFC3339), u
	})
	if !checked {
		t.Fatal("independent reader did not sample the partially seeded fixture")
	}

	// Reopen after the seeder's connection closes: prove durable complete rows,
	// including exact IDs, callback times, observation offsets and NULL raw_hex.
	reader, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	for table, want := range map[string]int{"transmissions": numTx, "observations": numTx * obsPerTx, "observers": 0, "nodes": 0, "schema_version": 1} {
		var count int
		if err := reader.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != want {
			t.Fatalf("%s count=%d, want %d", table, count, want)
		}
	}
	var version int
	if err := reader.QueryRow("SELECT version FROM schema_version").Scan(&version); err != nil || version != 1 {
		t.Fatalf("schema version=%d err=%v", version, err)
	}
	for i := 1; i <= numTx; i++ {
		u := epoch + int64(i)*60
		ts := time.Unix(u, 0).UTC().Format(time.RFC3339)
		var matched int
		if err := reader.QueryRow(`SELECT COUNT(*) FROM transmissions WHERE id=? AND raw_hex='aabb' AND hash=? AND first_seen=? AND route_type=0 AND payload_type=4 AND payload_version=1 AND decoded_json='{}' AND last_seen=?`, i, fmt.Sprintf("h%06d", i), ts, u).Scan(&matched); err != nil || matched != 1 {
			t.Fatalf("transmission %d exact fields: matched=%d err=%v", i, matched, err)
		}
		for j := 0; j < obsPerTx; j++ {
			id := (i-1)*obsPerTx + j + 1
			obsTime := time.Unix(u, 0).UTC().Add(-time.Duration(j) * time.Minute).Format(time.RFC3339)
			if err := reader.QueryRow(`SELECT COUNT(*) FROM observations WHERE id=? AND transmission_id=? AND observer_id='obs1' AND observer_name='Obs1' AND direction='RX' AND snr=-10 AND rssi=-80 AND score=5 AND path_json='[]' AND timestamp=? AND raw_hex IS NULL`, id, i, obsTime).Scan(&matched); err != nil || matched != 1 {
				t.Fatalf("observation %d exact fields: matched=%d err=%v", id, matched, err)
			}
		}
	}
}
