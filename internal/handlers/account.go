package handlers

import (
	"net/http"

	authmw "todo/internal/middleware"
	"todo/internal/user"
)

func (a *App) AccountUsername(w http.ResponseWriter, r *http.Request) {
	uid := authmw.UserID(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if err := user.Rename(r.Context(), a.DB, uid, r.FormValue("username")); err != nil {
		if isUserFacing(err) {
			a.renderSettings(w, r, http.StatusBadRequest, err.Error(), "")
			return
		}
		serverError(w, err)
		return
	}
	a.renderSettings(w, r, http.StatusOK, "", "Username diperbarui.")
}

func (a *App) AccountPassword(w http.ResponseWriter, r *http.Request) {
	uid := authmw.UserID(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if r.FormValue("new") != r.FormValue("confirm") {
		a.renderSettings(w, r, http.StatusBadRequest, "Konfirmasi password tidak sama.", "")
		return
	}
	// Percobaan menebak password lama lewat sesi yang tercuri dibatasi sama seperti login.
	if a.Limiter.Blocked(r) {
		a.renderSettings(w, r, http.StatusTooManyRequests, "Terlalu banyak percobaan. Coba lagi nanti.", "")
		return
	}
	if err := user.ChangePassword(r.Context(), a.DB, uid, r.FormValue("current"), r.FormValue("new")); err != nil {
		if isUserFacing(err) {
			if err == user.ErrPasswordWrong {
				a.Limiter.Fail(r)
			}
			a.renderSettings(w, r, http.StatusBadRequest, err.Error(), "")
			return
		}
		serverError(w, err)
		return
	}
	// Putuskan sesi di perangkat lain; sesi ini tetap login.
	if err := a.Sessions.DestroyOthers(r.Context(), r, uid); err != nil {
		serverError(w, err)
		return
	}
	a.renderSettings(w, r, http.StatusOK, "", "Password diganti. Perangkat lain dikeluarkan.")
}

func (a *App) AccountEmail(w http.ResponseWriter, r *http.Request) {
	uid := authmw.UserID(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if err := user.SetEmail(r.Context(), a.DB, uid, r.FormValue("email")); err != nil {
		if isUserFacing(err) {
			a.renderSettings(w, r, http.StatusBadRequest, err.Error(), "")
			return
		}
		serverError(w, err)
		return
	}
	a.renderSettings(w, r, http.StatusOK, "", "Email diperbarui.")
}
