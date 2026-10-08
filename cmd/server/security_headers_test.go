package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestSecurityHeadersMiddleware_AllServerResponsePaths(t *testing.T) {
	t.Parallel()

	wantHeaders := map[string]string{
		"Cross-Origin-Resource-Policy":      "same-origin",
		"Permissions-Policy":                browserPermissionsPolicy,
		"Referrer-Policy":                   "strict-origin-when-cross-origin",
		"Strict-Transport-Security":         "max-age=31536000",
		"X-Content-Type-Options":            "nosniff",
		"X-DNS-Prefetch-Control":            "off",
		"X-Permitted-Cross-Domain-Policies": "none",
	}

	tests := []struct {
		name   string
		path   string
		status int
	}{
		{name: "API response", path: "/api/health", status: http.StatusOK},
		{name: "static asset", path: "/app.js", status: http.StatusOK},
		{name: "SPA fallback", path: "/nodes/example", status: http.StatusOK},
		{name: "WebSocket handshake", path: "/ws", status: http.StatusSwitchingProtocols},
		{name: "error response", path: "/missing", status: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			})
			handler := securityHeadersMiddleware(next)
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if got, want := rr.Header().Get("Content-Security-Policy"), browserContentSecurityPolicyForHost(req.Host); got != want {
				t.Errorf("Content-Security-Policy = %q, want %q", got, want)
			}
			for name, want := range wantHeaders {
				if got := rr.Header().Get(name); got != want {
					t.Errorf("%s = %q, want %q", name, got, want)
				}
			}
		})
	}
}

func TestSecurityHeadersMiddleware_CSPAllowsSameHostWebSockets(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "https://corescope.example:8443/api/health", nil)
	rr := httptest.NewRecorder()
	securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, req)

	want := strings.Replace(
		browserContentSecurityPolicy,
		"connect-src 'self'",
		"connect-src 'self' ws://corescope.example:8443 wss://corescope.example:8443",
		1,
	)
	if got := rr.Header().Get("Content-Security-Policy"); got != want {
		t.Fatalf("Content-Security-Policy = %q, want %q", got, want)
	}
}

func TestSecurityHeadersMiddleware_AreaMapCrossOriginConnectionsAreRouteScoped(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		target               string
		allowCrossOriginHTTP bool
	}{
		{name: "exact area map path", target: "https://corescope.example:8443/area-map.html", allowCrossOriginHTTP: true},
		{name: "area map path with query", target: "https://corescope.example:8443/area-map.html?embed=1&server=remote", allowCrossOriginHTTP: true},
		{name: "ordinary API path", target: "https://corescope.example:8443/api/health"},
		{name: "ordinary SPA path", target: "https://corescope.example:8443/nodes/example"},
		{name: "area map child path", target: "https://corescope.example:8443/area-map.html/extra"},
		{name: "area map suffix lookalike", target: "https://corescope.example:8443/area-map.html.evil"},
		{name: "area map path parameter lookalike", target: "https://corescope.example:8443/area-map.html;extra"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.target, nil)
			rr := httptest.NewRecorder()
			securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})).ServeHTTP(rr, req)

			want := browserContentSecurityPolicyForHost(req.Host)
			if tt.allowCrossOriginHTTP {
				want = strings.Replace(want, "connect-src 'self'", "connect-src 'self' http: https:", 1)
			}
			if got := rr.Header().Get("Content-Security-Policy"); got != want {
				t.Fatalf("Content-Security-Policy = %q, want %q", got, want)
			}
		})
	}
}

func TestSecurityHeadersMiddleware_CSPRejectsInvalidHosts(t *testing.T) {
	t.Parallel()

	invalidHosts := []string{
		"",
		"corescope.example wss://evil.example",
		"corescope.example\r\nX-Injected: yes",
		"corescope.example; connect-src *",
		"corescope.example,evil.example",
		"user@corescope.example",
		"corescope.example/path",
		"*",
		"ws:",
		"wss:",
		"corescope.example:not-a-port",
		"corescope.example:70000",
		"2001:db8::1",
	}

	for _, host := range invalidHosts {
		host := host
		t.Run(host, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "https://corescope.example/", nil)
			req.Host = host
			rr := httptest.NewRecorder()
			securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})).ServeHTTP(rr, req)

			if got := rr.Header().Get("Content-Security-Policy"); got != browserContentSecurityPolicy {
				t.Fatalf("Content-Security-Policy for Host %q = %q, want fail-closed %q", host, got, browserContentSecurityPolicy)
			}
		})
	}
}

func TestSecurityHeadersMiddleware_CSPCanonicalizesSupportedHosts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		host string
		want string
	}{
		{name: "DNS", host: "CoreScope.Example", want: "corescope.example"},
		{name: "IPv4 with port", host: "192.0.2.10:8080", want: "192.0.2.10:8080"},
		{name: "bracketed IPv6", host: "[2001:0DB8::1]", want: "[2001:db8::1]"},
		{name: "bracketed IPv6 with port", host: "[2001:db8::1]:8443", want: "[2001:db8::1]:8443"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "https://corescope.example/", nil)
			req.Host = tt.host
			rr := httptest.NewRecorder()
			securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})).ServeHTTP(rr, req)

			want := strings.Replace(
				browserContentSecurityPolicy,
				"connect-src 'self'",
				"connect-src 'self' ws://"+tt.want+" wss://"+tt.want,
				1,
			)
			if got := rr.Header().Get("Content-Security-Policy"); got != want {
				t.Fatalf("Content-Security-Policy = %q, want %q", got, want)
			}
		})
	}
}

func TestSecurityHeadersMiddleware_PreservesWebSocketUpgrade(t *testing.T) {
	t.Parallel()

	hub := NewHub()
	defer hub.Close()
	server := httptest.NewServer(securityHeadersMiddleware(http.HandlerFunc(hub.ServeWS)))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, response, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial WebSocket through security middleware: %v", err)
	}
	defer conn.Close()

	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("WebSocket status = %d, want %d", response.StatusCode, http.StatusSwitchingProtocols)
	}
	wantCSP := browserContentSecurityPolicyForHost(response.Request.URL.Host)
	if got := response.Header.Get("Content-Security-Policy"); got != wantCSP {
		t.Errorf("WebSocket Content-Security-Policy = %q, want %q", got, wantCSP)
	}
	if got := response.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("WebSocket X-Content-Type-Options = %q, want %q", got, "nosniff")
	}
}

func TestBrowserContentSecurityPolicy_MatchesFrontendRequirements(t *testing.T) {
	t.Parallel()

	for _, required := range []string{
		"default-src 'self'",
		"base-uri 'self'",
		"object-src 'none'",
		"script-src 'self' 'unsafe-inline' https://unpkg.com",
		"style-src 'self' 'unsafe-inline' https://unpkg.com",
		"img-src 'self' data: blob: https:",
		"font-src 'self' data:",
		"connect-src 'self'",
		"media-src 'self' blob:",
		"worker-src 'self' blob:",
		"form-action 'self'",
	} {
		if !strings.Contains(browserContentSecurityPolicy, required) {
			t.Errorf("CSP missing frontend requirement %q: %s", required, browserContentSecurityPolicy)
		}
	}

	// CoreScope intentionally supports cross-origin map/channel embeds. Do not
	// silently disable that contract with either legacy or CSP frame blocking.
	if strings.Contains(browserContentSecurityPolicy, "frame-ancestors") {
		t.Fatalf("CSP must preserve the documented cross-origin embed mode: %s", browserContentSecurityPolicy)
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	rr := httptest.NewRecorder()
	securityHeadersMiddleware(next).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/#/map?embed=1", nil))
	if got := rr.Header().Get("X-Frame-Options"); got != "" {
		t.Fatalf("X-Frame-Options %q blocks CoreScope embed mode", got)
	}
}
