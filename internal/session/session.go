// Package session: minimal server-side session store (table-backed).
// Mirror pola journalflow: cookie hanya menyimpan session id, lookup ke tabel sessions.
package session

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/http"
	"time"
)

const (
	CookieName = "todo_session"
	ttl        = 30 * 24 * time.Hour
)

type Manager struct {
	db *sql.DB
}

func NewManager(db *sql.DB) *Manager {
	return &Manager{db: db}
}

func (m *Manager) Create(w http.ResponseWriter, r *http.Request, userID int64) error {
	id, err := randomID()
	if err != nil {
		return err
	}
	expires := time.Now().Add(ttl)
	_, err = m.db.ExecContext(r.Context(),
		`INSERT INTO sessions (id, user_id, expires_at) VALUES ($1, $2, $3)`,
		id, userID, expires)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    id,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	})
	return nil
}

func (m *Manager) UserID(r *http.Request) (int64, bool) {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return 0, false
	}
	var userID int64
	err = m.db.QueryRowContext(r.Context(),
		`SELECT user_id FROM sessions WHERE id = $1 AND expires_at > now()`,
		c.Value).Scan(&userID)
	if err != nil {
		return 0, false
	}
	return userID, true
}

func (m *Manager) Destroy(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(CookieName); err == nil {
		_, _ = m.db.ExecContext(context.Background(), `DELETE FROM sessions WHERE id = $1`, c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
	})
}

func randomID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
