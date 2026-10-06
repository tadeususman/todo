package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	authmw "todo/internal/middleware"
	"todo/internal/project"
)

func (a *App) ProjectCreate(w http.ResponseWriter, r *http.Request) {
	uid := authmw.UserID(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if _, err := project.Create(r.Context(), a.DB, uid, r.FormValue("name"), r.FormValue("color")); err != nil {
		projectError(w, err)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (a *App) ProjectRename(w http.ResponseWriter, r *http.Request) {
	uid := authmw.UserID(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if err := project.Rename(r.Context(), a.DB, uid, id, r.FormValue("name")); err != nil {
		projectError(w, err)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (a *App) ProjectDelete(w http.ResponseWriter, r *http.Request) {
	uid := authmw.UserID(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := project.Delete(r.Context(), a.DB, uid, id); err != nil {
		projectError(w, err)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}
