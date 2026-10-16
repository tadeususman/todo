package handlers

import (
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"todo/internal/user"
)

// resetEnabled: emailed reset needs both SMTP and a trusted public base URL for the link.
func (a *App) resetEnabled() bool { return a.Mail.Enabled() && a.PublicURL != "" }

func (a *App) ForgotPage(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, "forgot.html", map[string]any{})
}

const forgotReply = "Jika email itu terdaftar, tautan untuk mengatur ulang password sudah dikirim. Cek juga folder spam. Tautan berlaku 30 menit."

func (a *App) ForgotSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if !a.resetEnabled() {
		a.render(w, r, "forgot.html", map[string]any{})
		return
	}
	email := user.NormalizeEmail(r.FormValue("email"))
	// Setiap permintaan dihitung (per IP dan per email) supaya tidak bisa dipakai membanjiri kotak masuk orang lain.
	if a.ResetLimiter.Blocked(r) || a.ResetLimiter.BlockedKey("email:"+email) {
		a.render(w, r, "forgot.html", map[string]any{"_status": http.StatusTooManyRequests, "Error": "Terlalu banyak permintaan. Coba lagi nanti."})
		return
	}
	a.ResetLimiter.Fail(r)
	a.ResetLimiter.FailKey("email:" + email)

	if user.ValidateEmail(email) == nil {
		token, username, ok, err := user.CreateResetToken(r.Context(), a.DB, email)
		if err != nil {
			log.Printf("reset token: %v", err)
		} else if ok {
			a.audit(r, 0, "password_reset_requested", username)
			link := a.PublicURL + "/reset/" + token
			body := "Halo " + username + ",\n\nAda permintaan mengatur ulang password akun todo-mu. Buka tautan ini dalam 30 menit:\n\n" +
				link + "\n\nJika bukan kamu yang meminta, abaikan email ini; password-mu tidak berubah.\n"
			// Kirim di latar belakang agar waktu respons sama untuk email terdaftar maupun tidak.
			go func() {
				if err := a.Mail.Send(email, "Atur ulang password todo", body); err != nil {
					log.Printf("send reset mail: %v", err)
				}
			}()
		}
	}
	a.render(w, r, "forgot.html", map[string]any{"Info": forgotReply, "Sent": true})
}

func (a *App) ResetPage(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if !user.ResetTokenValid(r.Context(), a.DB, token) {
		a.render(w, r, "forgot.html", map[string]any{"_status": http.StatusGone, "Error": user.ErrResetInvalid.Error()})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	a.render(w, r, "reset.html", map[string]any{"Token": token})
}

func (a *App) ResetSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	token := strings.TrimSpace(r.FormValue("token"))
	if a.Limiter.Blocked(r) {
		a.render(w, r, "forgot.html", map[string]any{"_status": http.StatusTooManyRequests, "Error": "Terlalu banyak percobaan. Coba lagi nanti."})
		return
	}
	if r.FormValue("new") != r.FormValue("confirm") {
		a.render(w, r, "reset.html", map[string]any{"Token": token, "_status": http.StatusBadRequest, "Error": "Konfirmasi password tidak sama."})
		return
	}
	uid, err := user.ConsumeResetToken(r.Context(), a.DB, token, r.FormValue("new"))
	if err != nil {
		if err == user.ErrResetInvalid {
			a.Limiter.Fail(r)
			a.render(w, r, "forgot.html", map[string]any{"_status": http.StatusGone, "Error": err.Error()})
			return
		}
		if isUserFacing(err) {
			a.render(w, r, "reset.html", map[string]any{"Token": token, "_status": http.StatusBadRequest, "Error": err.Error()})
			return
		}
		serverError(w, err)
		return
	}
	a.audit(r, uid, "password_reset_done", "")
	a.render(w, r, "login.html", map[string]any{"Info": "Password diganti. Silakan masuk dengan password baru."})
}
