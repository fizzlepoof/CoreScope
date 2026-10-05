package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestInsertAnalyticsCacheBoundsEntries(t *testing.T) {
	cache := make(map[string]*cachedResult)
	expiresAt := time.Now().Add(time.Hour)

	for i := 0; i < analyticsCacheMaxEntries+1; i++ {
		insertAnalyticsCache(cache, fmt.Sprintf("key-%d", i), &cachedResult{expiresAt: expiresAt})
	}

	if got := len(cache); got != analyticsCacheMaxEntries {
		t.Fatalf("cache size = %d, want %d", got, analyticsCacheMaxEntries)
	}
}

func TestInsertAnalyticsCacheDropsExpiredEntries(t *testing.T) {
	cache := map[string]*cachedResult{
		"expired": {expiresAt: time.Now().Add(-time.Minute)},
		"live":    {expiresAt: time.Now().Add(time.Hour)},
	}

	insertAnalyticsCache(cache, "new", &cachedResult{expiresAt: time.Now().Add(time.Hour)})

	if _, ok := cache["expired"]; ok {
		t.Fatal("expired entry was not pruned")
	}
	if got := len(cache); got != 2 {
		t.Fatalf("cache size = %d, want 2 live entries", got)
	}
}

func TestInsertAnalyticsCacheSameKeyUpdateDoesNotEvict(t *testing.T) {
	cache := make(map[string]*cachedResult)
	for i := 0; i < analyticsCacheMaxEntries; i++ {
		cache[fmt.Sprintf("key-%d", i)] = &cachedResult{
			data:      map[string]interface{}{"version": "old"},
			expiresAt: time.Now().Add(time.Duration(i+1) * time.Hour),
		}
	}

	insertAnalyticsCache(cache, "key-31", &cachedResult{
		data:      map[string]interface{}{"version": "new"},
		expiresAt: time.Now().Add(48 * time.Hour),
	})

	if got := len(cache); got != analyticsCacheMaxEntries {
		t.Fatalf("cache size = %d, want %d", got, analyticsCacheMaxEntries)
	}
	if _, ok := cache["key-0"]; !ok {
		t.Fatal("same-key update evicted unrelated earliest entry")
	}
	if got := cache["key-31"].data["version"]; got != "new" {
		t.Fatalf("updated value = %v, want new", got)
	}
}

func TestAnalyticsOverlongFiltersReturnBadRequestBeforeComputation(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{name: "region", query: "region=" + strings.Repeat("r", 65)},
		{name: "area", query: "area=" + strings.Repeat("a", 65)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupTestDBv2(t)
			cfg := &Config{Areas: map[string]AreaEntry{strings.Repeat("a", 65): {Label: "Too long"}}}
			store := newTestStoreWithDB(t, db, cfg)
			srv := &Server{db: db, cfg: cfg, store: store}
			req := httptest.NewRequest(http.MethodGet, "/api/analytics/rf?"+tt.query, nil)
			rec := httptest.NewRecorder()

			srv.handleAnalyticsRF(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			if got := len(store.rfCache); got != 0 {
				t.Fatalf("RF cache entries = %d, want 0; analytics computation ran", got)
			}
		})
	}
}

func TestAnalyticsUnknownAreaReturnsBadRequestBeforeComputation(t *testing.T) {
	db := setupTestDBv2(t)
	cfg := &Config{Areas: map[string]AreaEntry{"KNOWN": {Label: "Known"}}}
	store := newTestStoreWithDB(t, db, cfg)
	srv := &Server{db: db, cfg: cfg, store: store}
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/rf?area=UNKNOWN", nil)
	rec := httptest.NewRecorder()

	srv.handleAnalyticsRF(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	var body ErrorResp
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if body.Error == "" {
		t.Fatal("response JSON has no error")
	}
	if got := len(store.rfCache); got != 0 {
		t.Fatalf("RF cache entries = %d, want 0; analytics computation ran", got)
	}
}

func TestAnalyticsAreaRejectedWithoutAreaConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
	}{
		{name: "nil config"},
		{name: "nil areas", cfg: &Config{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupTestDBv2(t)
			store := newTestStoreWithDB(t, db, tt.cfg)
			srv := &Server{db: db, cfg: tt.cfg, store: store}
			req := httptest.NewRequest(http.MethodGet, "/api/analytics/rf?area=ANY", nil)
			rec := httptest.NewRecorder()

			srv.handleAnalyticsRF(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			if got := len(store.rfCache); got != 0 {
				t.Fatalf("RF cache entries = %d, want 0; analytics computation ran", got)
			}
		})
	}
}

func TestAnalyticsRegionOnlyEndpointsRejectOverlongRegionBeforeIndexWork(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		handler func(*Server, http.ResponseWriter, *http.Request)
	}{
		{
			name:    "subpaths",
			path:    "/api/analytics/subpaths?region=" + strings.Repeat("r", 65),
			handler: (*Server).handleAnalyticsSubpaths,
		},
		{
			name:    "subpaths-bulk",
			path:    "/api/analytics/subpaths-bulk?groups=2-2:1&region=" + strings.Repeat("r", 65),
			handler: (*Server).handleAnalyticsSubpathsBulk,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupTestDBv2(t)
			store := newTestStoreWithDB(t, db, nil)
			srv := &Server{db: db, store: store}
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			tt.handler(srv, rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			if got := len(store.subpathCache); got != 0 {
				t.Fatalf("subpath cache entries = %d, want 0; computation ran", got)
			}
		})
	}
}

func TestAnalyticsAreaEndpointsAcceptValidAndEmptyArea(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		path string
	}{
		{
			name: "configured area",
			cfg:  &Config{Areas: map[string]AreaEntry{"KNOWN": {Label: "Known"}}},
			path: "/api/analytics/rf?area=KNOWN",
		},
		{
			name: "empty area without config",
			path: "/api/analytics/rf?region=not-in-config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := &Server{cfg: tt.cfg}
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			srv.handleAnalyticsRF(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestAnalyticsAreaEndpointsRejectUnknownAreaBeforeWork(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		handler func(*Server, http.ResponseWriter, *http.Request)
	}{
		{name: "rf", path: "/api/analytics/rf?area=UNKNOWN", handler: (*Server).handleAnalyticsRF},
		{name: "topology", path: "/api/analytics/topology?area=UNKNOWN", handler: (*Server).handleAnalyticsTopology},
		{name: "channels", path: "/api/analytics/channels?area=UNKNOWN", handler: (*Server).handleAnalyticsChannels},
		{name: "distance", path: "/api/analytics/distance?area=UNKNOWN", handler: (*Server).handleAnalyticsDistance},
		{name: "hash-sizes", path: "/api/analytics/hash-sizes?area=UNKNOWN", handler: (*Server).handleAnalyticsHashSizes},
		{name: "hash-collisions", path: "/api/analytics/hash-collisions?area=UNKNOWN", handler: (*Server).handleAnalyticsHashCollisions},
		{name: "subpaths", path: "/api/analytics/subpaths?area=UNKNOWN", handler: (*Server).handleAnalyticsSubpaths},
		{name: "subpaths-bulk", path: "/api/analytics/subpaths/bulk?area=UNKNOWN&groups=2-2:10", handler: (*Server).handleAnalyticsSubpathsBulk},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupTestDBv2(t)
			cfg := &Config{Areas: map[string]AreaEntry{"KNOWN": {Label: "Known"}}}
			store := newTestStoreWithDB(t, db, cfg)
			srv := &Server{db: db, cfg: cfg, store: store}
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			tt.handler(srv, rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			cacheEntries := len(store.rfCache) + len(store.topoCache) + len(store.chanCache) +
				len(store.distCache) + len(store.hashCache) + len(store.collisionCache) + len(store.subpathCache)
			if cacheEntries != 0 {
				t.Fatalf("analytics cache entries = %d, want 0; computation ran", cacheEntries)
			}
			if store.DistanceIndexBuilt() {
				t.Fatal("distance index was built before filter rejection")
			}
		})
	}
}

func TestNeighborGraphRejectsOverlongRegionBeforeWork(t *testing.T) {
	called := false
	srv := &Server{
		computeNeighborGraphResponseFn: func(int, float64, string, string) NeighborGraphResponse {
			called = true
			return NeighborGraphResponse{}
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/neighbor-graph?region="+strings.Repeat("r", 65), nil)
	rec := httptest.NewRecorder()

	srv.handleNeighborGraph(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatal("neighbor graph computation ran before region rejection")
	}
}
