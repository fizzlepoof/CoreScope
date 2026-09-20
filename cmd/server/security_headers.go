package main

import (
	"net"
	"net/http"
	"strconv"
	"strings"
)

// browserContentSecurityPolicy reflects the frontend's current runtime needs:
// inline script/style is still used throughout the no-build UI, Leaflet and
// Chart.js are loaded from unpkg, maps and operator-configured branding may
// load HTTPS images, QR codes use data: images, and the live feed connects to
// the same origin over WebSocket. Keep this policy in sync with public/.
//
// frame-ancestors is intentionally omitted: CoreScope supports cross-origin
// map and channel embeds. The same compatibility requirement is why this
// middleware does not emit X-Frame-Options.
const browserContentSecurityPolicy = "default-src 'self'; base-uri 'self'; object-src 'none'; script-src 'self' 'unsafe-inline' https://unpkg.com; style-src 'self' 'unsafe-inline' https://unpkg.com; img-src 'self' data: blob: https:; font-src 'self' data:; connect-src 'self'; media-src 'self' blob:; worker-src 'self' blob:; frame-src 'none'; form-action 'self'; manifest-src 'self'"

// Camera access powers the channel QR scanner. Clipboard write is used by the
// share/copy controls; the remaining sensitive browser capabilities are not
// used by CoreScope and are disabled.
const browserPermissionsPolicy = "accelerometer=(), camera=(self), clipboard-write=(self), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()"

func browserContentSecurityPolicyForHost(rawHost string) string {
	host, ok := canonicalCSPHost(rawHost)
	if !ok {
		return browserContentSecurityPolicy
	}
	return strings.Replace(
		browserContentSecurityPolicy,
		"connect-src 'self'",
		"connect-src 'self' ws://"+host+" wss://"+host,
		1,
	)
}

func browserContentSecurityPolicyForRequest(r *http.Request) string {
	policy := browserContentSecurityPolicyForHost(r.Host)
	if r.URL.Path != "/area-map.html" {
		return policy
	}

	// The standalone area-map tool accepts an operator-entered CoreScope base
	// URL, so only this exact page may fetch APIs from arbitrary HTTP(S) origins.
	return strings.Replace(policy, "connect-src 'self'", "connect-src 'self' http: https:", 1)
}

func canonicalCSPHost(rawHost string) (string, bool) {
	if rawHost == "" {
		return "", false
	}

	host, port := rawHost, ""
	if strings.HasPrefix(rawHost, "[") {
		closingBracket := strings.IndexByte(rawHost, ']')
		if closingBracket < 0 {
			return "", false
		}
		ip := net.ParseIP(rawHost[1:closingBracket])
		if ip == nil || ip.To4() != nil {
			return "", false
		}
		host = "[" + ip.String() + "]"
		remainder := rawHost[closingBracket+1:]
		if remainder != "" {
			if !strings.HasPrefix(remainder, ":") {
				return "", false
			}
			port = remainder[1:]
		}
	} else {
		if strings.Count(rawHost, ":") > 1 {
			return "", false
		}
		if before, after, found := strings.Cut(rawHost, ":"); found {
			host, port = before, after
		}
		if ip := net.ParseIP(host); ip != nil {
			if ip.To4() == nil {
				return "", false
			}
			host = ip.String()
		} else if !validDNSName(host) {
			return "", false
		} else {
			host = strings.ToLower(host)
		}
	}

	if port == "" {
		if strings.HasSuffix(rawHost, ":") {
			return "", false
		}
		return host, true
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return "", false
	}
	return host + ":" + strconv.Itoa(portNumber), true
}

func validDNSName(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-') {
				return false
			}
		}
	}
	return true
}

// securityHeadersMiddleware applies browser hardening to every server response,
// including API errors, static/SPA responses, and WebSocket upgrade handshakes.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", browserContentSecurityPolicyForRequest(r))
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Permissions-Policy", browserPermissionsPolicy)
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Strict-Transport-Security", "max-age=31536000")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-DNS-Prefetch-Control", "off")
		h.Set("X-Permitted-Cross-Domain-Policies", "none")
		next.ServeHTTP(w, r)
	})
}
