package admindb

import (
	"errors"
	"sync"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// The private, per-store seam is configured before authentication starts. The
// observer still executes real bcrypt; no timing thresholds or global hooks.
type passwordComparisonObserver interface {
	setPasswordComparer(func([]byte, []byte) error)
}

func TestAuthenticatePerformsBCryptComparison(t *testing.T) {
	s := openTestStore(t)
	const password = "comparison-password"
	if _, err := s.CreateAdmin("known", password, RoleAdmin, nil); err != nil {
		t.Fatal(err)
	}
	var storedHash string
	if err := s.db.QueryRow(`SELECT password_hash FROM admins WHERE username = 'known'`).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	observer, ok := interface{}(s).(passwordComparisonObserver)
	if !ok {
		t.Fatal("Store lacks a private per-store bcrypt comparison seam: actual unknown-user work cannot be observed")
	}
	var mu sync.Mutex
	type comparison struct {
		hash, password string
		err            error
	}
	var calls []comparison
	observer.setPasswordComparer(func(hash, candidate []byte) error {
		err := bcrypt.CompareHashAndPassword(hash, candidate)
		mu.Lock()
		calls = append(calls, comparison{string(hash), string(candidate), err})
		mu.Unlock()
		return err
	})
	for _, tc := range []struct {
		name, username, candidate, hash string
		valid                           bool
	}{
		{"unknown", "missing", password, invalidCredentialHash, false},
		{"known wrong password", "known", "incorrect-password", storedHash, false},
		{"known correct password", "known", password, storedHash, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mu.Lock()
			calls = nil
			mu.Unlock()
			admin, err := s.Authenticate(tc.username, tc.candidate)
			if tc.valid {
				if err != nil || admin == nil {
					t.Fatalf("valid authentication: admin=%v err=%v", admin, err)
				}
			} else if admin != nil || err != ErrInvalidCredentials {
				t.Fatalf("invalid authentication: admin=%v err=%v", admin, err)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(calls) != 1 {
				t.Fatalf("bcrypt comparisons = %d, want exactly 1", len(calls))
			}
			call := calls[0]
			if call.hash != tc.hash || call.password != tc.candidate {
				t.Fatal("bcrypt did not compare the expected stored/dummy hash and supplied password")
			}
			cost, err := bcrypt.Cost([]byte(call.hash))
			if err != nil || cost != bcryptCost {
				t.Fatalf("compared hash cost = %d, err=%v; want %d", cost, err, bcryptCost)
			}
			if tc.valid && call.err != nil || !tc.valid && !errors.Is(call.err, bcrypt.ErrMismatchedHashAndPassword) {
				t.Fatalf("real bcrypt result = %v, valid=%v", call.err, tc.valid)
			}
		})
	}
	// Concurrent calls share only this Store's immutable comparer. Recording is
	// synchronized independently; bcrypt remains outside database locks.
	mu.Lock()
	calls = nil
	mu.Unlock()
	var workers sync.WaitGroup
	for i := 0; i < 4; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if admin, err := s.Authenticate("missing", password); admin != nil || err != ErrInvalidCredentials {
				t.Errorf("concurrent authentication: admin=%v err=%v", admin, err)
			}
		}()
	}
	workers.Wait()
	if len(calls) != 4 {
		t.Fatalf("concurrent bcrypt comparisons = %d, want 4", len(calls))
	}
	// An independently opened Store must not inherit the observer.
	other := openTestStore(t)
	if admin, err := other.Authenticate("missing", password); admin != nil || err != ErrInvalidCredentials {
		t.Fatalf("other Store: admin=%v err=%v", admin, err)
	}
	if len(calls) != 4 {
		t.Fatal("comparison observer leaked to another Store")
	}
}
