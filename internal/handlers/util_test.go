package handlers

import (
	"strconv"
	"strings"
	"testing"
	"time"

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

func TestDayLabelAndChatItems(t *testing.T) {
	at := func(y int, m time.Month, d, h, mi int) time.Time { return time.Date(y, m, d, h, mi, 0, 0, wibLoc) }
	now := at(2026, time.October, 7, 15, 0)
	cases := map[time.Time]string{
		at(2026, time.October, 7, 0, 5):   "Hari ini",
		at(2026, time.October, 6, 23, 59): "Kemarin",
		at(2026, time.October, 5, 12, 0):  "Senin, 5 Okt",
		at(2025, time.December, 31, 9, 0): "Rabu, 31 Des 2025",
	}
	for in, want := range cases {
		if got := dayLabel(in, now); got != want {
			t.Errorf("dayLabel(%v) = %q, want %q", in, got, want)
		}
	}
	// 23:30 UTC on the 6th is already the 7th in WIB
	if got := dayLabel(time.Date(2026, time.October, 6, 23, 30, 0, 0, time.UTC), now); got != "Hari ini" {
		t.Errorf("UTC->WIB day: %q", got)
	}

	msgs := []chat.Message{
		{ID: 1, CreatedAt: at(2026, time.October, 6, 9, 0)},
		{ID: 2, CreatedAt: at(2026, time.October, 6, 23, 59)},
		{ID: 3, CreatedAt: at(2026, time.October, 7, 0, 1)},
		{ID: 4, CreatedAt: at(2026, time.October, 7, 8, 0)},
	}
	items := chatItems(msgs, now)
	want := []string{"Kemarin", "", "Hari ini", ""}
	for i, it := range items {
		if it.Day != want[i] {
			t.Errorf("item %d day = %q, want %q", i, it.Day, want[i])
		}
	}
	if len(chatItems(nil, now)) != 0 {
		t.Error("empty")
	}
}
