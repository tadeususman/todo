package handlers

import (
	"strconv"
	"strings"
	"testing"

	"todo/internal/chat"
)

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

func TestParseIDs(t *testing.T) {
	ids, ok := parseIDs([]string{"3", " 5 ", "3", "x", "-1", "0", "", "7"})
	if !ok || len(ids) != 3 || ids[0] != 3 || ids[1] != 5 || ids[2] != 7 {
		t.Errorf("parseIDs = %v, %v", ids, ok)
	}
	var many []string
	for i := 1; i <= maxBulkIDs+1; i++ {
		many = append(many, strconv.Itoa(i))
	}
	if _, ok := parseIDs(many); ok {
		t.Error("expected ok=false above maxBulkIDs")
	}
	if ids, ok := parseIDs(many[:maxBulkIDs]); !ok || len(ids) != maxBulkIDs {
		t.Error("exactly maxBulkIDs should pass")
	}
}

func TestChatMsgRendersActions(t *testing.T) {
	pages := LoadTemplates("../../web/templates")
	m := chat.Message{
		ID: 7, Role: chat.RoleAssistant, Content: "Siap", ActionStatus: chat.ActionPending,
		Actions: []chat.Action{{TaskID: 12, Title: "<b>Revisi</b> laporan", Field: "priority", Value: "medium"}},
	}
	render := func(m chat.Message) string {
		var b strings.Builder
		if err := pages["chat.html"].ExecuteTemplate(&b, "chat-msg", m); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	out := render(m)
	for _, want := range []string{`/chat/7/apply`, `/chat/7/dismiss`, `Priority → medium`, `href="/tasks/12"`, `&lt;b&gt;Revisi&lt;/b&gt; laporan`} {
		if !strings.Contains(out, want) {
			t.Errorf("pending render missing %q", want)
		}
	}
	if strings.Contains(out, "<b>Revisi") {
		t.Error("task title not escaped")
	}
	m.ActionStatus = chat.ActionApplied
	if out := render(m); strings.Contains(out, "/apply") || !strings.Contains(out, "Sudah diterapkan") {
		t.Error("applied message must not offer buttons")
	}
	m.Actions = nil
	if out := render(m); strings.Contains(out, "msg-actions") {
		t.Error("plain message shows action card")
	}
}
