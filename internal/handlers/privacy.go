package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	authmw "todo/internal/middleware"
	"todo/internal/user"
)

func (a *App) PrivacyPage(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, "privacy.html", map[string]any{"Nav": "settings", "SupportEmail": a.SupportEmail, "NotifyOn": a.Notify.Enabled()})
}

// jsonRows runs a query that selects one json array column and returns it raw.
func jsonRows(r *http.Request, db *sql.DB, query string, uid int64) (json.RawMessage, error) {
	var raw []byte
	if err := db.QueryRowContext(r.Context(), query, uid).Scan(&raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// AccountExport downloads everything stored about the logged-in user as JSON (no password hash, no other users' data).
func (a *App) AccountExport(w http.ResponseWriter, r *http.Request) {
	uid := authmw.UserID(r)
	me, err := user.Get(r.Context(), a.DB, uid)
	if err != nil {
		serverError(w, err)
		return
	}
	out := map[string]any{
		"exported_at": time.Now().UTC().Format(time.RFC3339),
		"account":     map[string]any{"username": me.Username, "email": me.Email, "created_at": me.CreatedAt},
	}
	queries := []struct{ key, q string }{
		{"projects", `SELECT coalesce(json_agg(x ORDER BY id), '[]') FROM (SELECT id, name, color, is_default, created_at FROM projects WHERE user_id = $1) x`},
		{"tasks", `SELECT coalesce(json_agg(x ORDER BY id), '[]') FROM (SELECT id, parent_id, project_id, title, description, status, priority, complexity, deadline, estimated_minutes, completed_at, archived_at, created_at, updated_at FROM tasks WHERE user_id = $1) x`},
		{"chat", `SELECT coalesce(json_agg(x ORDER BY id), '[]') FROM (SELECT id, role, content, created_at FROM chat_messages WHERE user_id = $1) x`},
		{"preferences", `SELECT coalesce(json_agg(x), '[]') FROM (SELECT theme, ui_font, ui_font_size FROM user_prefs WHERE user_id = $1) x`},
	}
	for _, q := range queries {
		raw, err := jsonRows(r, a.DB, q.q, uid)
		if err != nil {
			serverError(w, err)
			return
		}
		out[q.key] = raw
	}
	a.audit(r, 0, "data_export", "")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="todo-data-`+time.Now().Format("2006-01-02")+`.json"`)
	w.Header().Set("Cache-Control", "no-store")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

// AccountDelete permanently deletes the account and all its data after re-entering the password.
func (a *App) AccountDelete(w http.ResponseWriter, r *http.Request) {
	uid := authmw.UserID(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if a.Limiter.Blocked(r) {
		a.renderSettings(w, r, http.StatusTooManyRequests, "Terlalu banyak percobaan. Coba lagi nanti.", "")
		return
	}
	if err := user.DeleteAccount(r.Context(), a.DB, uid, r.FormValue("password")); err != nil {
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
	a.audit(r, 0, "account_deleted", "")
	a.Sessions.Destroy(w, r)
	a.render(w, r, "login.html", map[string]any{"Info": "Akunmu dan seluruh datanya sudah dihapus."})
}

// AdminAudit shows the most recent security events.
func (a *App) AdminAudit(w http.ResponseWriter, r *http.Request) {
	type row struct {
		When   time.Time
		Actor  string
		Action string
		Target string
		IP     string
	}
	rows, err := a.DB.QueryContext(r.Context(), `
		SELECT l.created_at, coalesce(u.username, '—'), l.action, l.target, l.ip
		FROM audit_log l LEFT JOIN users u ON u.id = l.actor_id
		ORDER BY l.id DESC LIMIT 200`)
	if err != nil {
		serverError(w, err)
		return
	}
	defer rows.Close()
	var list []row
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.When, &x.Actor, &x.Action, &x.Target, &x.IP); err != nil {
			serverError(w, err)
			return
		}
		list = append(list, x)
	}
	a.render(w, r, "admin_audit.html", map[string]any{"Nav": "settings", "Rows": list})
}
