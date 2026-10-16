package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	authmw "todo/internal/middleware"
	"todo/internal/push"
)

type subscribeBody struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

func decodeSub(w http.ResponseWriter, r *http.Request) (subscribeBody, bool) {
	var b subscribeBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&b); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return b, false
	}
	return b, true
}

func (a *App) PushSubscribe(w http.ResponseWriter, r *http.Request) {
	b, ok := decodeSub(w, r)
	if !ok {
		return
	}
	if err := a.Push.Subscribe(r.Context(), authmw.UserID(r), b.Endpoint, b.Keys.P256dh, b.Keys.Auth); err != nil {
		if err == push.ErrBadEndpoint {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) PushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	b, ok := decodeSub(w, r)
	if !ok {
		return
	}
	if err := a.Push.Unsubscribe(r.Context(), authmw.UserID(r), b.Endpoint); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PushTest sends a sample notification so the user can confirm the setup works.
func (a *App) PushTest(w http.ResponseWriter, r *http.Request) {
	uid := authmw.UserID(r)
	if a.PushTestLimiter.BlockedKey(strconv.FormatInt(uid, 10)) {
		http.Error(w, "Terlalu sering. Coba lagi nanti.", http.StatusTooManyRequests)
		return
	}
	a.PushTestLimiter.FailKey(strconv.FormatInt(uid, 10))
	n, err := a.Push.Send(r.Context(), uid, push.Message{Title: "Notifikasi todo aktif", Body: "Pengingat deadline akan muncul seperti ini.", URL: "/settings"})
	if err != nil {
		serverError(w, err)
		return
	}
	if n == 0 {
		http.Error(w, "Tidak ada perangkat yang berhasil dikirimi.", http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) PrefsSetRemind(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	m, _ := strconv.Atoi(r.FormValue("minutes"))
	if err := push.SetLead(r.Context(), a.DB, authmw.UserID(r), m); err != nil {
		serverError(w, err)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}
