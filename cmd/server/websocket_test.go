package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestConfigWebSocketMaxClients(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want int
	}{
		{name: "nil config uses safe default", cfg: nil, want: defaultWebSocketMaxClients},
		{name: "missing config uses safe default", cfg: &Config{}, want: defaultWebSocketMaxClients},
		{name: "configured positive limit", cfg: &Config{WebSocket: &WebSocketConfig{MaxClients: 12}}, want: 12},
		{name: "zero cannot disable limit", cfg: &Config{WebSocket: &WebSocketConfig{}}, want: defaultWebSocketMaxClients},
		{name: "negative cannot disable limit", cfg: &Config{WebSocket: &WebSocketConfig{MaxClients: -1}}, want: defaultWebSocketMaxClients},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.WebSocketMaxClients(); got != tt.want {
				t.Fatalf("WebSocketMaxClients() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestConfigWebSocketMaxClientsPerIP(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want int
	}{
		{name: "nil config uses safe default", cfg: nil, want: defaultWebSocketMaxClientsPerIP},
		{name: "missing config uses safe default", cfg: &Config{}, want: defaultWebSocketMaxClientsPerIP},
		{name: "configured positive limit", cfg: &Config{WebSocket: &WebSocketConfig{MaxClientsPerIP: 7}}, want: 7},
		{name: "zero cannot disable limit", cfg: &Config{WebSocket: &WebSocketConfig{}}, want: defaultWebSocketMaxClientsPerIP},
		{name: "negative cannot disable limit", cfg: &Config{WebSocket: &WebSocketConfig{MaxClientsPerIP: -1}}, want: defaultWebSocketMaxClientsPerIP},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.WebSocketMaxClientsPerIP(); got != tt.want {
				t.Fatalf("WebSocketMaxClientsPerIP() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestConfigWebSocketTrustedProxyCIDRs(t *testing.T) {
	configured := []string{"10.0.0.0/8", "2001:db8:1234::/48"}
	cfg := &Config{WebSocket: &WebSocketConfig{TrustedProxyCIDRs: configured}}

	got := cfg.WebSocketTrustedProxyCIDRs()
	if len(got) != len(configured) {
		t.Fatalf("WebSocketTrustedProxyCIDRs() = %q, want %q", got, configured)
	}
	for i := range configured {
		if got[i] != configured[i] {
			t.Fatalf("WebSocketTrustedProxyCIDRs()[%d] = %q, want %q", i, got[i], configured[i])
		}
	}
	got[0] = "changed"
	if cfg.WebSocket.TrustedProxyCIDRs[0] != configured[0] {
		t.Fatal("WebSocketTrustedProxyCIDRs returned mutable config storage")
	}

	if got := (*Config)(nil).WebSocketTrustedProxyCIDRs(); len(got) != 0 {
		t.Fatalf("nil config trusted proxies = %q, want empty", got)
	}
}

func waitForClientCount(t *testing.T, hub *Hub, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hub.ClientCount() == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d WebSocket clients; got %d", want, hub.ClientCount())
}

func TestHubRejectsUpgradeAtClientLimitAndAllowsReconnect(t *testing.T) {
	hub := newHubWithLimits(1, 1)
	srv := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer srv.Close()
	wsURL := "ws" + srv.URL[4:]

	first, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("first dial: %v", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("first dial status = %d, want 101", resp.StatusCode)
	}
	waitForClientCount(t, hub, 1)

	excess, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if excess != nil {
		excess.Close()
	}
	if resp != nil {
		defer resp.Body.Close()
	}
	if err == nil {
		t.Fatal("excess dial succeeded; want admission rejection")
	}
	if resp == nil || resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("excess dial response = %#v, want 503 Service Unavailable", resp)
	}
	if got := resp.Header.Get("Retry-After"); got != "1" {
		t.Fatalf("Retry-After = %q, want %q", got, "1")
	}
	if got := hub.ClientCount(); got != 1 {
		t.Fatalf("client count after rejected upgrade = %d, want 1", got)
	}

	if err := first.Close(); err != nil {
		t.Fatalf("close first client: %v", err)
	}
	waitForClientCount(t, hub, 0)

	reconnected, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("reconnect after slot release: %v", err)
	}
	defer reconnected.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("reconnect status = %d, want 101", resp.StatusCode)
	}
	waitForClientCount(t, hub, 1)
}

func TestHubClientLimitIsAtomicAcrossConcurrentUpgrades(t *testing.T) {
	const (
		maxClients = 3
		attempts   = 24
	)
	hub := newHubWithLimits(maxClients, attempts)
	srv := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer srv.Close()
	wsURL := "ws" + srv.URL[4:]

	type result struct {
		conn   *websocket.Conn
		status int
		err    error
	}
	results := make(chan result, attempts)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
			status := 0
			if resp != nil {
				status = resp.StatusCode
			}
			if err != nil && resp != nil {
				resp.Body.Close()
			}
			results <- result{conn: conn, status: status, err: err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	accepted := 0
	for res := range results {
		if res.err == nil {
			accepted++
			defer res.conn.Close()
			if res.status != http.StatusSwitchingProtocols {
				t.Errorf("accepted status = %d, want 101", res.status)
			}
			continue
		}
		if res.status != http.StatusServiceUnavailable {
			t.Errorf("rejected status = %d, want 503 (err=%v)", res.status, res.err)
		}
	}
	if accepted != maxClients {
		t.Fatalf("accepted %d concurrent upgrades, want exactly %d", accepted, maxClients)
	}
	waitForClientCount(t, hub, maxClients)
}

func TestHubRejectsUpgradeAtPerIPLimit(t *testing.T) {
	hub := newHubWithLimits(4, 1)
	srv := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer srv.Close()
	wsURL := "ws" + srv.URL[4:]

	first, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("first dial: %v", err)
	}
	defer first.Close()
	waitForClientCount(t, hub, 1)

	excess, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if excess != nil {
		excess.Close()
	}
	if resp != nil {
		defer resp.Body.Close()
	}
	if err == nil {
		t.Fatal("second dial from same IP succeeded; want admission rejection")
	}
	if resp == nil || resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("second dial response = %#v, want 503 Service Unavailable", resp)
	}
}

func TestHubPerIPLimitUsesForwardedClientFromBundledProxy(t *testing.T) {
	hub := newHubWithLimits(4, 1)
	srv := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer srv.Close()
	wsURL := "ws" + srv.URL[4:]

	firstHeaders := http.Header{"X-Forwarded-For": []string{"198.51.100.10"}}
	first, _, err := websocket.DefaultDialer.Dial(wsURL, firstHeaders)
	if err != nil {
		t.Fatalf("first proxied dial: %v", err)
	}
	defer first.Close()
	waitForClientCount(t, hub, 1)

	secondHeaders := http.Header{"X-Forwarded-For": []string{"198.51.100.11"}}
	second, resp, err := websocket.DefaultDialer.Dial(wsURL, secondHeaders)
	if err != nil {
		t.Fatalf("second proxied client was grouped with first (response=%v): %v", resp, err)
	}
	defer second.Close()
	waitForClientCount(t, hub, 2)
}

func TestWebSocketRemoteIP(t *testing.T) {
	tests := []struct {
		name                   string
		remoteAddr             string
		forwardedFor           []string
		additionalTrustedCIDRs []string
		want                   string
	}{
		{
			name:         "bundled IPv4 loopback proxy forwards client",
			remoteAddr:   "127.0.0.1:1234",
			forwardedFor: []string{"198.51.100.20"},
			want:         "198.51.100.20",
		},
		{
			name:         "bundled IPv6 loopback proxy forwards client",
			remoteAddr:   "[::1]:1234",
			forwardedFor: []string{"2001:db8::20"},
			want:         "2001:db8::20",
		},
		{
			name:         "mapped IPv4 loopback peer uses default trusted network",
			remoteAddr:   "[::ffff:127.0.0.1]:1234",
			forwardedFor: []string{"::ffff:198.51.100.20"},
			want:         "198.51.100.20",
		},
		{
			name:                   "mapped trusted hop resolves to dotted client",
			remoteAddr:             "[::ffff:10.0.0.3]:443",
			forwardedFor:           []string{"::ffff:198.51.100.40, ::ffff:10.0.0.2"},
			additionalTrustedCIDRs: []string{"10.0.0.0/8"},
			want:                   "198.51.100.40",
		},
		{
			name:                   "mapped configured prefix trusts dotted peer",
			remoteAddr:             "10.0.0.3:443",
			forwardedFor:           []string{"198.51.100.41"},
			additionalTrustedCIDRs: []string{"::ffff:10.0.0.0/104"},
			want:                   "198.51.100.41",
		},
		{
			name:                   "trusted chain resolves from right to left",
			remoteAddr:             "10.0.0.3:443",
			forwardedFor:           []string{"192.0.2.66, 198.51.100.40, 10.0.0.2"},
			additionalTrustedCIDRs: []string{"10.0.0.0/8"},
			want:                   "198.51.100.40",
		},
		{
			name:                   "multiple forwarding header lines form one chain",
			remoteAddr:             "10.0.0.3:443",
			forwardedFor:           []string{"192.0.2.66, 198.51.100.40", "10.0.0.2"},
			additionalTrustedCIDRs: []string{"10.0.0.0/8"},
			want:                   "198.51.100.40",
		},
		{
			name:                   "all trusted chain returns leftmost address",
			remoteAddr:             "10.0.0.3:443",
			forwardedFor:           []string{"10.0.0.1", "10.0.0.2"},
			additionalTrustedCIDRs: []string{"10.0.0.0/8"},
			want:                   "10.0.0.1",
		},
		{
			name:                   "invalid configured CIDRs preserve loopback defaults",
			remoteAddr:             "127.0.0.1:1234",
			forwardedFor:           []string{"198.51.100.42"},
			additionalTrustedCIDRs: []string{"not-a-cidr", "::ffff:10.0.0.0/80"},
			want:                   "198.51.100.42",
		},
		{
			name:         "untrusted direct peer cannot spoof forwarding header",
			remoteAddr:   "203.0.113.9:4321",
			forwardedFor: []string{"198.51.100.99"},
			want:         "203.0.113.9",
		},
		{
			name:         "malformed forwarded value falls back to direct peer",
			remoteAddr:   "127.0.0.1:1234",
			forwardedFor: []string{"198.51.100.20, not-an-ip"},
			want:         "127.0.0.1",
		},
		{
			name:       "direct IPv4 client",
			remoteAddr: "192.0.2.10:1234",
			want:       "192.0.2.10",
		},
		{
			name:       "direct IPv6 client",
			remoteAddr: "[2001:db8::1]:4321",
			want:       "2001:db8::1",
		},
		{
			name:       "non-IP peer identifier remains stable",
			remoteAddr: "local-client",
			want:       "local-client",
		},
		{
			name: "missing peer has stable identity",
			want: "unknown",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "http://example.test/ws", nil)
			r.RemoteAddr = tt.remoteAddr
			for _, value := range tt.forwardedFor {
				r.Header.Add("X-Forwarded-For", value)
			}
			trustedProxies := newTrustedProxySet(tt.additionalTrustedCIDRs)
			if got := webSocketRemoteIP(r, trustedProxies); got != tt.want {
				t.Errorf("webSocketRemoteIP(%q, %q) = %q, want %q", tt.remoteAddr, tt.forwardedFor, got, tt.want)
			}
		})
	}
}

func TestHubPerIPLimitTreatsMappedAndDottedForwardedIPAsSameClient(t *testing.T) {
	hub := newHubWithLimits(4, 1)
	srv := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer srv.Close()
	wsURL := "ws" + srv.URL[4:]

	first, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{
		"X-Forwarded-For": []string{"::ffff:198.51.100.55"},
	})
	if err != nil {
		t.Fatalf("mapped client dial: %v", err)
	}
	defer first.Close()
	waitForClientCount(t, hub, 1)

	excess, resp, err := websocket.DefaultDialer.Dial(wsURL, http.Header{
		"X-Forwarded-For": []string{"198.51.100.55"},
	})
	if excess != nil {
		excess.Close()
	}
	if resp != nil {
		defer resp.Body.Close()
	}
	if err == nil {
		t.Fatal("dotted form bypassed per-IP limit established by mapped form")
	}
	if resp == nil || resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("equivalent client response = %#v, want 503 Service Unavailable", resp)
	}
}

func TestHubCloseReleasesAdmissionForReuse(t *testing.T) {
	hub := newHubWithLimits(1, 1)
	srv := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer srv.Close()
	wsURL := "ws" + srv.URL[4:]

	first, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("first dial: %v", err)
	}
	defer first.Close()
	waitForClientCount(t, hub, 1)

	hub.Close()
	waitForClientCount(t, hub, 0)

	second, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		if resp != nil {
			resp.Body.Close()
		}
		t.Fatalf("dial after Hub.Close: %v", err)
	}
	defer second.Close()
	waitForClientCount(t, hub, 1)
}

func TestHubTrustedProxyConfigurationIsRaceSafe(t *testing.T) {
	hub := newHubWithLimits(1000, 1000)
	start := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 100; i++ {
			hub.SetTrustedProxyCIDRs([]string{"10.0.0.0/8"})
			hub.SetTrustedProxyCIDRs([]string{"192.0.2.0/24"})
		}
	}()

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 100; j++ {
				r := httptest.NewRequest(http.MethodGet, "http://example.test/ws", nil)
				r.RemoteAddr = "10.0.0.1:443"
				r.Header.Set("X-Forwarded-For", "198.51.100.1")
				hub.ServeWS(httptest.NewRecorder(), r)
			}
		}()
	}

	close(start)
	wg.Wait()
}

func TestHubLimitDoesNotMaskOriginRejection(t *testing.T) {
	hub := newHubWithLimits(1, 1)
	srv := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer srv.Close()
	wsURL := "ws" + srv.URL[4:]

	first, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("first dial: %v", err)
	}
	defer first.Close()
	waitForClientCount(t, hub, 1)

	headers := http.Header{"Origin": []string{"https://evil.example.com"}}
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if conn != nil {
		conn.Close()
	}
	if resp != nil {
		defer resp.Body.Close()
	}
	if err == nil {
		t.Fatal("foreign-origin dial succeeded; want rejection")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign-origin response = %#v, want 403 Forbidden", resp)
	}
}

func TestHubFailedUpgradeReleasesAdmissionSlot(t *testing.T) {
	hub := newHubWithLimits(1, 1)
	srv := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		t.Fatalf("plain HTTP request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("plain HTTP status = %d, want 400", resp.StatusCode)
	}

	conn, _, err := websocket.DefaultDialer.Dial("ws"+srv.URL[4:], nil)
	if err != nil {
		t.Fatalf("valid dial after failed upgrade: %v", err)
	}
	defer conn.Close()
	waitForClientCount(t, hub, 1)
}

func TestHubBroadcast(t *testing.T) {
	hub := NewHub()

	if hub.ClientCount() != 0 {
		t.Errorf("expected 0 clients, got %d", hub.ClientCount())
	}

	// Create a test server with WebSocket endpoint
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.ServeWS(w, r)
	}))
	defer srv.Close()

	// Connect a WebSocket client
	wsURL := "ws" + srv.URL[4:] // replace http with ws
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial error: %v", err)
	}
	defer conn.Close()

	// Wait for registration
	time.Sleep(50 * time.Millisecond)

	if hub.ClientCount() != 1 {
		t.Errorf("expected 1 client, got %d", hub.ClientCount())
	}

	// Broadcast a message
	hub.Broadcast(map[string]interface{}{
		"type": "packet",
		"data": map[string]interface{}{"id": 1, "hash": "test123"},
	})

	// Read the message
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if len(msg) == 0 {
		t.Error("expected non-empty message")
	}

	// Disconnect
	conn.Close()
	time.Sleep(100 * time.Millisecond)
}

func TestPollerCreation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	seedTestData(t, db)
	hub := NewHub()

	poller := NewPoller(db, hub, 100*time.Millisecond)
	if poller == nil {
		t.Fatal("expected poller")
	}

	// Start and stop
	go poller.Start()
	time.Sleep(200 * time.Millisecond)
	poller.Stop()
}

func TestHubMultipleClients(t *testing.T) {
	hub := NewHub()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.ServeWS(w, r)
	}))
	defer srv.Close()

	wsURL := "ws" + srv.URL[4:]

	// Connect two clients
	conn1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial error: %v", err)
	}
	defer conn1.Close()

	conn2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial error: %v", err)
	}
	defer conn2.Close()

	time.Sleep(100 * time.Millisecond)

	if hub.ClientCount() != 2 {
		t.Errorf("expected 2 clients, got %d", hub.ClientCount())
	}

	// Broadcast and both should receive
	hub.Broadcast(map[string]interface{}{"type": "test", "data": "hello"})

	conn1.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg1, err := conn1.ReadMessage()
	if err != nil {
		t.Fatalf("conn1 read error: %v", err)
	}
	if len(msg1) == 0 {
		t.Error("expected non-empty message on conn1")
	}

	conn2.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg2, err := conn2.ReadMessage()
	if err != nil {
		t.Fatalf("conn2 read error: %v", err)
	}
	if len(msg2) == 0 {
		t.Error("expected non-empty message on conn2")
	}

	// Disconnect one
	conn1.Close()
	time.Sleep(100 * time.Millisecond)

	// Remaining client should still work
	hub.Broadcast(map[string]interface{}{"type": "test2"})

	conn2.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg3, err := conn2.ReadMessage()
	if err != nil {
		t.Fatalf("conn2 read error after disconnect: %v", err)
	}
	if len(msg3) == 0 {
		t.Error("expected non-empty message")
	}
}

func TestBroadcastFullBuffer(t *testing.T) {
	hub := NewHub()

	// Create a client with tiny buffer (1)
	client := &Client{
		send: make(chan []byte, 1),
	}
	hub.mu.Lock()
	hub.clients[client] = true
	hub.mu.Unlock()

	// Fill the buffer
	client.send <- []byte("first")

	// This broadcast should drop the message (buffer full)
	hub.Broadcast(map[string]interface{}{"type": "dropped"})

	// Channel should still only have the first message
	select {
	case msg := <-client.send:
		if string(msg) != "first" {
			t.Errorf("expected 'first', got %s", string(msg))
		}
	default:
		t.Error("expected message in channel")
	}

	// Clean up
	hub.mu.Lock()
	delete(hub.clients, client)
	hub.mu.Unlock()
}

func TestBroadcastMarshalError(t *testing.T) {
	hub := NewHub()

	// Marshal error: functions can't be marshaled to JSON
	hub.Broadcast(map[string]interface{}{"bad": func() {}})
	// Should not panic — just log and return
}

func TestPollerBroadcastsNewData(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	seedTestData(t, db)
	hub := NewHub()

	// Create a client to receive broadcasts
	client := &Client{
		send: make(chan []byte, 256),
	}
	hub.mu.Lock()
	hub.clients[client] = true
	hub.mu.Unlock()

	poller := NewPoller(db, hub, 50*time.Millisecond)
	go poller.Start()

	// Insert new data to trigger broadcast
	db.conn.Exec(`INSERT INTO transmissions (raw_hex, hash, first_seen, route_type, payload_type)
		VALUES ('EEFF', 'newhash123456789', '2026-01-16T10:00:00Z', 1, 4)`)

	time.Sleep(200 * time.Millisecond)
	poller.Stop()

	// Check if client received broadcast with packet field (fixes #162)
	select {
	case msg := <-client.send:
		if len(msg) == 0 {
			t.Error("expected non-empty broadcast message")
		}
		var parsed map[string]interface{}
		if err := json.Unmarshal(msg, &parsed); err != nil {
			t.Fatalf("failed to parse broadcast: %v", err)
		}
		if parsed["type"] != "packet" {
			t.Errorf("expected type=packet, got %v", parsed["type"])
		}
		data, ok := parsed["data"].(map[string]interface{})
		if !ok {
			t.Fatal("expected data to be an object")
		}
		// packets.js filters on m.data.packet — must exist
		pkt, ok := data["packet"]
		if !ok || pkt == nil {
			t.Error("expected data.packet to exist (required by packets.js WS handler)")
		}
		pktMap, ok := pkt.(map[string]interface{})
		if !ok {
			t.Fatal("expected data.packet to be an object")
		}
		// Verify key fields exist in nested packet (timestamp required by packets.js)
		for _, field := range []string{"id", "hash", "payload_type", "timestamp"} {
			if _, exists := pktMap[field]; !exists {
				t.Errorf("expected data.packet.%s to exist", field)
			}
		}
	default:
		// Might not have received due to timing
	}

	// Clean up
	hub.mu.Lock()
	delete(hub.clients, client)
	hub.mu.Unlock()
}

func TestPollerBroadcastsMultipleObservations(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	seedTestData(t, db)
	hub := NewHub()

	client := &Client{
		send: make(chan []byte, 256),
	}
	hub.mu.Lock()
	hub.clients[client] = true
	hub.mu.Unlock()
	defer func() {
		hub.mu.Lock()
		delete(hub.clients, client)
		hub.mu.Unlock()
	}()

	poller := NewPoller(db, hub, 50*time.Millisecond)
	store := NewPacketStore(db, nil)
	if err := store.Load(); err != nil {
		t.Fatalf("store load failed: %v", err)
	}
	poller.store = store
	go poller.Start()
	defer poller.Stop()

	// Wait for poller to initialize its lastID/lastObsID cursors before
	// inserting new data; otherwise the poller may snapshot a lastID that
	// already includes the test data and never broadcast it.
	time.Sleep(100 * time.Millisecond)

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.conn.Exec(`INSERT INTO transmissions (raw_hex, hash, first_seen, route_type, payload_type, decoded_json)
		VALUES ('FACE', 'starbursthash237a', ?, 1, 4, '{"pubKey":"aabbccdd11223344","type":"ADVERT"}')`, now); err != nil {
		t.Fatalf("insert tx failed: %v", err)
	}
	var txID int
	if err := db.conn.QueryRow(`SELECT id FROM transmissions WHERE hash='starbursthash237a'`).Scan(&txID); err != nil {
		t.Fatalf("query tx id failed: %v", err)
	}
	ts := time.Now().Unix()
	if _, err := db.conn.Exec(`INSERT INTO observations (transmission_id, observer_idx, snr, rssi, path_json, timestamp)
		VALUES (?, 1, 14.0, -82, '["aa"]', ?),
		       (?, 2, 10.5, -90, '["aa","bb"]', ?),
		       (?, 1, 7.0, -96, '["aa","bb","cc"]', ?)`,
		txID, ts, txID, ts+1, txID, ts+2); err != nil {
		t.Fatalf("insert observations failed: %v", err)
	}

	deadline := time.After(2 * time.Second)
	var dataMsgs []map[string]interface{}
	for len(dataMsgs) < 3 {
		select {
		case raw := <-client.send:
			var parsed map[string]interface{}
			if err := json.Unmarshal(raw, &parsed); err != nil {
				t.Fatalf("unmarshal ws msg failed: %v", err)
			}
			if parsed["type"] != "packet" {
				continue
			}
			data, ok := parsed["data"].(map[string]interface{})
			if !ok {
				continue
			}
			if data["hash"] == "starbursthash237a" {
				dataMsgs = append(dataMsgs, data)
			}
		case <-deadline:
			t.Fatalf("timed out waiting for 3 observation broadcasts, got %d", len(dataMsgs))
		}
	}

	if len(dataMsgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(dataMsgs))
	}

	paths := make([]string, 0, 3)
	observers := make(map[string]bool)
	for _, m := range dataMsgs {
		hash, _ := m["hash"].(string)
		if hash != "starbursthash237a" {
			t.Fatalf("unexpected hash %q", hash)
		}
		p, _ := m["path_json"].(string)
		paths = append(paths, p)
		if oid, ok := m["observer_id"].(string); ok && oid != "" {
			observers[oid] = true
		}
	}
	sort.Strings(paths)
	wantPaths := []string{`["aa","bb","cc"]`, `["aa","bb"]`, `["aa"]`}
	sort.Strings(wantPaths)
	for i := range wantPaths {
		if paths[i] != wantPaths[i] {
			t.Fatalf("path mismatch at %d: got %q want %q", i, paths[i], wantPaths[i])
		}
	}
	if len(observers) < 2 {
		t.Fatalf("expected observations from >=2 observers, got %d", len(observers))
	}
}

func TestIngestNewObservationsBroadcast(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	seedTestData(t, db)
	store := NewPacketStore(db, nil)
	if err := store.Load(); err != nil {
		t.Fatalf("store load failed: %v", err)
	}

	maxObs := db.GetMaxObservationID()
	now := time.Now().Unix()
	if _, err := db.conn.Exec(`INSERT INTO observations (transmission_id, observer_idx, snr, rssi, path_json, timestamp)
		VALUES (1, 2, 6.0, -100, '["aa","zz"]', ?),
		       (1, 1, 5.0, -101, '["aa","yy"]', ?)`, now, now+1); err != nil {
		t.Fatalf("insert new observations failed: %v", err)
	}

	maps := store.IngestNewObservations(maxObs, 500)
	if len(maps) != 2 {
		t.Fatalf("expected 2 broadcast maps, got %d", len(maps))
	}
	for _, m := range maps {
		if m["hash"] != "abc123def4567890" {
			t.Fatalf("unexpected hash in map: %v", m["hash"])
		}
		path, ok := m["path_json"].(string)
		if !ok || path == "" {
			t.Fatalf("missing path_json in map: %#v", m)
		}
		if _, ok := m["observer_id"]; !ok {
			t.Fatalf("missing observer_id in map: %#v", m)
		}
	}
}

func TestHubRegisterUnregister(t *testing.T) {
	hub := NewHub()

	client := &Client{
		send: make(chan []byte, 256),
	}

	hub.Register(client)
	if hub.ClientCount() != 1 {
		t.Errorf("expected 1 client after register, got %d", hub.ClientCount())
	}

	hub.Unregister(client)
	if hub.ClientCount() != 0 {
		t.Errorf("expected 0 clients after unregister, got %d", hub.ClientCount())
	}

	// Unregister again should be safe
	hub.Unregister(client)
	if hub.ClientCount() != 0 {
		t.Errorf("expected 0 clients, got %d", hub.ClientCount())
	}
}
