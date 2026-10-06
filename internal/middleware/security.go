package middleware

import (
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

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

func clientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
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
