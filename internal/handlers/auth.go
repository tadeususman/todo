package handlers

import (
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"
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
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")

	var userID int64
	var hash string
	err := a.DB.QueryRowContext(r.Context(),
		`SELECT id, password_hash FROM users WHERE username = $1`, username).
		Scan(&userID, &hash)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		a.render(w, r, "login.html", map[string]any{"Error": "Username atau password salah."})
		return
	}
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
