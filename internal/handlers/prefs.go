package handlers

import (
	"net/http"

	authmw "todo/internal/middleware"
	"todo/internal/prefs"
)

func (a *App) PrefsSetTheme(w http.ResponseWriter, r *http.Request) {
	uid := authmw.UserID(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if err := prefs.SetTheme(r.Context(), a.DB, uid, r.FormValue("theme")); err != nil {
		serverError(w, err)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (a *App) PrefsSetFont(w http.ResponseWriter, r *http.Request) {
	uid := authmw.UserID(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if err := prefs.SetFont(r.Context(), a.DB, uid, r.FormValue("font")); err != nil {
		serverError(w, err)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}
