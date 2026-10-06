package handlers

import (
	"errors"
	"net/http"

	"todo/internal/user"
)

func (a *App) LoginPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.Sessions.UserID(r); ok {
		http.Redirect(w, r, "/home", http.StatusSeeOther)
		return
	}
	a.render(w, r, "login.html", map[string]any{})
}

func (a *App) LoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if a.Limiter.Blocked(r) {
		a.render(w, r, "login.html", map[string]any{"_status": http.StatusTooManyRequests, "Error": "Terlalu banyak percobaan. Coba lagi 15 menit lagi."})
		return
	}
	u, err := user.Authenticate(r.Context(), a.DB, r.FormValue("username"), r.FormValue("password"))
	if err != nil {
		a.Limiter.Fail(r)
		a.render(w, r, "login.html", map[string]any{"Error": "Username atau password salah."})
		return
	}
	// Status dicek setelah password benar, supaya status akun orang lain tidak bisa diintip.
	switch u.Status {
	case user.StatusPending:
		a.render(w, r, "login.html", map[string]any{"Info": "Akunmu sedang menunggu persetujuan admin. Coba masuk lagi nanti."})
		return
	case user.StatusRejected:
		a.render(w, r, "login.html", map[string]any{"Error": "Pendaftaran akun ini ditolak admin."})
		return
	}
	userID := u.ID
	a.Limiter.Reset(r)
	if err := a.Sessions.Create(w, r, userID); err != nil {
		http.Error(w, "could not start session", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/home", http.StatusSeeOther)
}

func (a *App) Logout(w http.ResponseWriter, r *http.Request) {
	a.Sessions.Destroy(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *App) RegisterPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.Sessions.UserID(r); ok {
		http.Redirect(w, r, "/home", http.StatusSeeOther)
		return
	}
	a.render(w, r, "register.html", map[string]any{})
}

func (a *App) RegisterSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	// Setiap percobaan dihitung (bukan hanya yang gagal) supaya tidak bisa dipakai membanjiri daftar pending.
	if a.RegLimiter.Blocked(r) {
		a.render(w, r, "register.html", map[string]any{"_status": http.StatusTooManyRequests, "Error": "Terlalu banyak pendaftaran dari jaringan ini. Coba lagi nanti."})
		return
	}
	a.RegLimiter.Fail(r)

	username := r.FormValue("username")
	data := map[string]any{"Username": username, "Email": r.FormValue("email")}
	if r.FormValue("password") != r.FormValue("confirm") {
		data["Error"] = "Konfirmasi password tidak sama."
		a.render(w, r, "register.html", data)
		return
	}
	if err := user.Register(r.Context(), a.DB, username, r.FormValue("email"), r.FormValue("password")); err != nil {
		if isUserFacing(err) {
			data["Error"] = err.Error()
			a.render(w, r, "register.html", data)
			return
		}
		serverError(w, err)
		return
	}
	a.render(w, r, "register_done.html", map[string]any{})
}

// isUserFacing reports whether err is a validation error whose message is safe to show.
func isUserFacing(err error) bool {
	for _, e := range []error{user.ErrUsernameInvalid, user.ErrUsernameTaken, user.ErrEmailInvalid, user.ErrEmailTaken, user.ErrPasswordShort, user.ErrPasswordLong, user.ErrPasswordWrong} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}
