package handlers

import "testing"

func TestSafeRedirect(t *testing.T) {
	cases := map[string]string{
		"/todo":        "/todo",
		"//evil.com":   "/home",
		"/\\evil.com":  "/home",
		"https://x.io": "/home",
		"":             "/home",
	}
	for in, want := range cases {
		if got := safeRedirect(in, "/home"); got != want {
			t.Errorf("safeRedirect(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoadTemplates(t *testing.T) {
	pages := LoadTemplates("../../web/templates")
	for _, n := range []string{"home.html", "todo.html", "chat.html", "login.html", "settings.html", "task_detail.html"} {
		if pages[n] == nil {
			t.Errorf("missing page %s", n)
		}
	}
}
