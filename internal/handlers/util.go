package handlers

import (
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/lib/pq"

	"todo/internal/project"
)

// serverError logs the real error and returns a generic message so DB/internal details don't leak.
func serverError(w http.ResponseWriter, err error) {
	log.Printf("handler error: %v", err)
	http.Error(w, "Terjadi kesalahan, coba lagi.", http.StatusInternalServerError)
}

// projectError maps project-layer errors to a user-facing status.
func projectError(w http.ResponseWriter, err error) {
	var pqErr *pq.Error
	switch {
	case errors.Is(err, project.ErrNameEmpty):
		http.Error(w, "Nama project tidak boleh kosong.", http.StatusBadRequest)
	case errors.Is(err, project.ErrDeleteDefault):
		http.Error(w, "Project default tidak bisa dihapus.", http.StatusBadRequest)
	case errors.As(err, &pqErr) && pqErr.Code == "23505":
		http.Error(w, "Nama project sudah dipakai.", http.StatusConflict)
	default:
		serverError(w, err)
	}
}

// safeRedirect only allows same-site absolute paths ("/foo"), rejecting "//host" and "/\host".
func safeRedirect(target, fallback string) string {
	if !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") || strings.HasPrefix(target, "/\\") {
		return fallback
	}
	return target
}

// maxBulkIDs bounds one bulk action so a crafted request can't build a huge query.
const maxBulkIDs = 200

// parseIDs turns form values into unique positive IDs (order kept), silently dropping junk.
// ok is false when more than maxBulkIDs distinct IDs were sent.
func parseIDs(vals []string) (ids []int64, ok bool) {
	seen := map[int64]bool{}
	for _, v := range vals {
		id, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil || id <= 0 || seen[id] {
			continue
		}
		if len(ids) == maxBulkIDs {
			return nil, false
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids, true
}

const flashCookie = "todo_flash"

// maxTaskInput bounds what we send to the LLM (cost + abuse guard).
const maxTaskInput = 500

// setFlash stores a one-shot message shown on the next page render.
func setFlash(w http.ResponseWriter, msg string) {
	http.SetCookie(w, &http.Cookie{Name: flashCookie, Value: url.QueryEscape(msg), Path: "/", MaxAge: 60, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

// popFlash reads and clears the flash message.
func popFlash(w http.ResponseWriter, r *http.Request) string {
	c, err := r.Cookie(flashCookie)
	if err != nil {
		return ""
	}
	http.SetCookie(w, &http.Cookie{Name: flashCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	msg, err := url.QueryUnescape(c.Value)
	if err != nil || len(msg) > 400 {
		return ""
	}
	return msg
}

// backTo returns the same-host path (+query) of the Referer so actions return to the page they came from.
func backTo(r *http.Request, fallback string) string {
	u, err := url.Parse(r.Referer())
	if err != nil || u.Host != r.Host || u.Path == "" {
		return fallback
	}
	return safeRedirect(u.RequestURI(), fallback)
}
