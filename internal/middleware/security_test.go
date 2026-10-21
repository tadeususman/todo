package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRedirectHosts(t *testing.T) {
	h := RedirectHosts("todo.tade.my.id, Old.Example", "https://todo-app.biz.id")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	cases := []struct {
		method, host, uri string
		code              int
		loc               string
	}{
		{"GET", "todo.tade.my.id", "/todo?q=a%20b", http.StatusMovedPermanently, "https://todo-app.biz.id/todo?q=a%20b"},
		{"GET", "old.example:443", "/", http.StatusMovedPermanently, "https://todo-app.biz.id/"},
		{"POST", "todo.tade.my.id", "/login", http.StatusPermanentRedirect, "https://todo-app.biz.id/login"},
		{"GET", "todo-app.biz.id", "/home", http.StatusTeapot, ""},
		{"GET", "127.0.0.1:8083", "/healthz", http.StatusTeapot, ""},
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, c.uri, nil)
		r.Host = c.host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.code || w.Header().Get("Location") != c.loc {
			t.Errorf("%s %s%s: got %d %q, want %d %q", c.method, c.host, c.uri, w.Code, w.Header().Get("Location"), c.code, c.loc)
		}
	}
}

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

func TestClientIPCloudflareTunnel(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "127.0.0.1:50000" // cloudflared on the same host
	r.Header.Set("CF-Connecting-IP", "198.51.100.20")
	r.Header.Set("X-Real-IP", "6.6.6.6")
	if got := clientIP(r); got != "198.51.100.20" {
		t.Errorf("got %s, want the Cloudflare-reported client", got)
	}
	r.RemoteAddr = "203.0.113.9:1" // direct internet peer must not be able to claim an address
	if got := clientIP(r); got != "203.0.113.9" {
		t.Errorf("spoofed CF-Connecting-IP accepted from untrusted peer: %s", got)
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
