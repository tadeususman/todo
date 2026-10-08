package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	authmw "todo/internal/middleware"
	"todo/internal/user"
)

func (a *App) AdminUsers(w http.ResponseWriter, r *http.Request) {
	a.renderAdminUsers(w, r, 0, "")
}

// renderAdminUsers menampilkan daftar user; resetID/resetPW (jika ada) menampilkan password sementara sekali.
func (a *App) renderAdminUsers(w http.ResponseWriter, r *http.Request, resetID int64, resetPW string) {
	users, err := user.List(r.Context(), a.DB)
	if err != nil {
		serverError(w, err)
		return
	}
	a.render(w, r, "admin_users.html", map[string]any{
		"Nav":     "settings",
		"Users":   users,
		"Pending": user.PendingCount(r.Context(), a.DB),
		"Me":      authmw.UserID(r),
		"ResetID": resetID,
		"ResetPW": resetPW,
	})
}

func (a *App) adminSetStatus(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if err := user.SetStatus(r.Context(), a.DB, id, status); err != nil {
			if err == user.ErrNotFound {
				http.NotFound(w, r)
				return
			}
			serverError(w, err)
			return
		}
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
	}
}

func (a *App) AdminApprove() http.HandlerFunc { return a.adminSetStatus(user.StatusApproved) }
func (a *App) AdminReject() http.HandlerFunc  { return a.adminSetStatus(user.StatusRejected) }

// AdminResetPassword membuat password sementara untuk user dan menampilkannya sekali (tanpa redirect, tidak disimpan).
func (a *App) AdminResetPassword(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	pw, err := user.ResetPassword(r.Context(), a.DB, id)
	if err != nil {
		if err == user.ErrNotFound {
			http.NotFound(w, r)
			return
		}
		serverError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	a.renderAdminUsers(w, r, id, pw)
}
