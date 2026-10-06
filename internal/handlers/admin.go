package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	authmw "todo/internal/middleware"
	"todo/internal/user"
)

func (a *App) AdminUsers(w http.ResponseWriter, r *http.Request) {
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
