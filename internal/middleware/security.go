package middleware

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"todo/internal/session"
)

// csp is as strict as the current templates allow: they still use inline <script>/<style> and style attributes,
// and load htmx from unpkg and web fonts from Google. Tighten with nonces once the inline code is gone.
const csp = "default-src 'self'; " +
	"script-src 'self' 'unsafe-inline' https://unpkg.com; " +
	"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
	"font-src 'self' https://fonts.gstatic.com; " +
	"img-src 'self' data:; " +
	"connect-src 'self'; " +
	"manifest-src 'self'; worker-src 'self'; " +
	"object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"

// SecurityHeaders sets baseline browser hardening headers on every response.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		if session.IsSecure(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

// MaxBody caps request body size so a single client can't exhaust memory with huge forms.
func MaxBody(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, n)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SameOrigin rejects state-changing requests whose Origin/Referer host differs from the request Host (CSRF guard).
// Requests without either header (non-browser clients) are allowed; browsers always send one on cross-site POSTs.
func SameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		src := r.Header.Get("Origin")
		if src == "" {
			src = r.Header.Get("Referer")
		}
		if src != "" {
			u, err := url.Parse(src)
			if err != nil || u.Host != r.Host {
				http.Error(w, "Cross-site request ditolak.", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// LoginLimiter allows `max` failed attempts per IP within `window`; success resets the counter.
type LoginLimiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string][]time.Time
}

func NewLoginLimiter(max int, window time.Duration) *LoginLimiter {
	return &LoginLimiter{max: max, window: window, hits: map[string][]time.Time{}}
}

// trustedProxies lists networks allowed to set X-Real-IP. Default: loopback and private ranges,
// i.e. a reverse proxy on the host or docker network, never a client on the public internet.
var trustedProxies = mustParseCIDRs("127.0.0.0/8", "::1/128", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7")

func mustParseCIDRs(cidrs ...string) []*net.IPNet {
	nets, err := ParseCIDRs(cidrs)
	if err != nil {
		panic(err)
	}
	return nets
}

// ParseCIDRs parses CIDRs or bare IPs.
func ParseCIDRs(items []string) ([]*net.IPNet, error) {
	var nets []*net.IPNet
	for _, it := range items {
		it = strings.TrimSpace(it)
		if it == "" {
			continue
		}
		if !strings.Contains(it, "/") {
			if strings.Contains(it, ":") {
				it += "/128"
			} else {
				it += "/32"
			}
		}
		_, n, err := net.ParseCIDR(it)
		if err != nil {
			return nil, err
		}
		nets = append(nets, n)
	}
	return nets, nil
}

// SetTrustedProxies overrides the default trusted proxy networks.
func SetTrustedProxies(nets []*net.IPNet) { trustedProxies = nets }

func isTrusted(ip net.IP) bool {
	for _, n := range trustedProxies {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// clientIP returns the peer address, or X-Real-IP only when the peer is a trusted proxy.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if xr := r.Header.Get("X-Real-IP"); xr != "" {
		if peer := net.ParseIP(host); peer != nil && isTrusted(peer) {
			if ip := net.ParseIP(strings.TrimSpace(xr)); ip != nil {
				return ip.String()
			}
		}
	}
	return host
}

func (l *LoginLimiter) recent(ip string, now time.Time) []time.Time {
	kept := l.hits[ip][:0]
	for _, t := range l.hits[ip] {
		if now.Sub(t) < l.window {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.hits, ip)
	} else {
		l.hits[ip] = kept
	}
	return kept
}

// Blocked reports whether the client has exhausted its attempts.
func (l *LoginLimiter) Blocked(r *http.Request) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recent(clientIP(r), time.Now())) >= l.max
}

func (l *LoginLimiter) Fail(r *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	ip := clientIP(r)
	l.hits[ip] = append(l.recent(ip, time.Now()), time.Now())
}

func (l *LoginLimiter) Reset(r *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hits, clientIP(r))
}
