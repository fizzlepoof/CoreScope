package main

import (
	"bytes"
	"database/sql"
	"log"
	"strings"
	"sync"
	"testing"
)

// capturedLogBuffer protects both logger writes and test snapshots. The
// standard logger serializes writes, but not readers of its output buffer.
// Keep the underlying buffer private so all access uses the same mutex.
type capturedLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *capturedLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *capturedLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureLogs redirects the standard logger to a buffer for the
// duration of the test and returns the buffer. Restores the previous
// writer when the test ends.
func captureLogs(t *testing.T) *capturedLogBuffer {
	t.Helper()
	buf := &capturedLogBuffer{}
	prevWriter := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(buf)
	t.Cleanup(func() {
		log.SetOutput(prevWriter)
		log.SetFlags(prevFlags)
	})
	return buf
}

// logContains reports whether the captured log buffer contains substr
// (case-insensitive).
func logContains(buf *capturedLogBuffer, substr string) bool {
	return strings.Contains(strings.ToLower(buf.String()), strings.ToLower(substr))
}

// columnExists reports whether the named column exists on the table.
func columnExists(t *testing.T, db *sql.DB, table, col string) bool {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatalf("PRAGMA table_info(%s): %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dfltValue sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			t.Fatalf("scan PRAGMA: %v", err)
		}
		if name == col {
			return true
		}
	}
	return false
}
