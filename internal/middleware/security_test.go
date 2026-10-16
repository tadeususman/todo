package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientIPTrustsHeaderOnlyFromProxy(t *testing.T) {
	cases := []struct {
		name, remote, xreal, want string
	}{
		{"public peer spoofing header", "203.0.113.9:5555", "1.2.3.4", "203.0.113.9"},
		{"docker proxy forwards real ip", "172.18.0.5:4444", "198.51.100.7", "198.51.100.7"},
		{"loopback proxy, junk header", "127.0.0.1:1", "not-an-ip", "127.0.0.1"},
		{"no header", "172.18.0.5:4444", "", "172.18.0.5"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remote
		if c.xreal != "" {
			r.Header.Set("X-Real-IP", c.xreal)
		}
		if got := clientIP(r); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := SecurityHeaders(nilHandler{})
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	for _, k := range []string{"Content-Security-Policy", "X-Frame-Options", "X-Content-Type-Options", "Strict-Transport-Security"} {
		if w.Header().Get(k) == "" {
			t.Errorf("missing %s", k)
		}
	}
}

type nilHandler struct{}

func (nilHandler) ServeHTTP(http.ResponseWriter, *http.Request) {}

func TestSameOriginBlocksCrossSitePost(t *testing.T) {
	h := SameOrigin(nilHandler{})
	cases := []struct {
		name, method, origin, referer string
		want                          int
	}{
		{"cross-site origin", "POST", "https://evil.example", "", 403},
		{"cross-site referer", "POST", "", "https://evil.example/x", 403},
		{"same origin", "POST", "http://todo.test", "", 200},
		{"GET never blocked", "GET", "https://evil.example", "", 200},
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, "http://todo.test/tasks/1/delete", nil)
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if c.referer != "" {
			r.Header.Set("Referer", c.referer)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Errorf("%s: got %d want %d", c.name, w.Code, c.want)
		}
	}
}

func TestLoginLimiterByKey(t *testing.T) {
	l := NewLoginLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		l.FailKey("alice")
	}
	if !l.BlockedKey("alice") || l.BlockedKey("bob") {
		t.Error("limiter must block only the failing key")
	}
	l.ResetKey("alice")
	if l.BlockedKey("alice") {
		t.Error("reset should unblock")
	}
}
