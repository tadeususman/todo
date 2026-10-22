package notify

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestSendPostsToBotAPI(t *testing.T) {
	var path string
	var form url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = r.ParseForm()
		form = r.PostForm
	}))
	defer srv.Close()

	tg := New("TOKEN", "-100123")
	tg.api = srv.URL
	tg.send("halo <b>& dunia")
	if path != "/botTOKEN/sendMessage" || form.Get("chat_id") != "-100123" || form.Get("text") != "halo <b>& dunia" {
		t.Errorf("path=%q form=%v", path, form)
	}
}

func TestDisabledIsNoop(t *testing.T) {
	var nilTG *Telegram
	nilTG.Send("x") // must not panic
	if New("", "1").Enabled() || New("t", "").Enabled() || nilTG.Enabled() {
		t.Error("incomplete config reported as enabled")
	}
	if !New("t", "1").Enabled() {
		t.Error("complete config reported as disabled")
	}
}
