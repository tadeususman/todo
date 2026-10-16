package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
