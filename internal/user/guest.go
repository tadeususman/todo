package user

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"todo/internal/prefs"
	"todo/internal/project"
	"todo/internal/task"
)

const (
	GuestTTL      = 24 * time.Hour
	GuestAILimit  = 8   // panggilan AI (parse task + chat) per akun tamu
	MaxGuests     = 300 // batas akun tamu aktif; menahan banjir pembuatan akun dari banyak IP
	guestAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"
)

var (
	ErrTooManyGuests = errors.New("Mode coba sedang penuh. Coba lagi nanti atau daftar akun.")
	ErrNotGuest      = errors.New("bukan akun tamu")
)

// CreateGuest makes a temporary approved account with a few sample tasks.
// The password hash is unusable ("!"), so the account can only be reached through the session we create.
func CreateGuest(ctx context.Context, db *sql.DB) (int64, error) {
	var active int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE guest_until > now()`).Scan(&active); err != nil {
		return 0, err
	}
	if active >= MaxGuests {
		return 0, ErrTooManyGuests
	}
	until := time.Now().Add(GuestTTL)
	var id int64
	for range 5 {
		buf := make([]byte, 6)
		if _, err := rand.Read(buf); err != nil {
			return 0, err
		}
		name := make([]byte, len(buf))
		for i, b := range buf {
			name[i] = guestAlphabet[int(b)%len(guestAlphabet)]
		}
		err := db.QueryRowContext(ctx,
			`INSERT INTO users (username, password_hash, status, guest_until) VALUES ($1, '!', 'approved', $2) RETURNING id`,
			"tamu-"+string(name), until).Scan(&id)
		if uniqueErr(err) != nil {
			continue
		}
		if err != nil {
			return 0, err
		}
		if err := seedGuest(ctx, db, id); err != nil {
			_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, id)
			return 0, err
		}
		return id, nil
	}
	return 0, errors.New("could not allocate guest username")
}

func seedGuest(ctx context.Context, db *sql.DB, id int64) error {
	if err := project.EnsureDefault(ctx, db, id); err != nil {
		return err
	}
	if err := prefs.EnsureRow(ctx, db, id); err != nil {
		return err
	}
	pid, err := project.DefaultID(ctx, db, id)
	if err != nil {
		return err
	}
	now := time.Now()
	at := func(h int) *time.Time { t := now.Add(time.Duration(h) * time.Hour); return &t }
	for _, p := range []*task.Parsed{
		{Title: "Coba tambah task: ketik \"rapat tim besok jam 10\"", Priority: "medium", Complexity: "simple", Deadline: at(3)},
		{Title: "Tanya asisten di tab Chat", Priority: "low", Complexity: "simple"},
		{Title: "Selesaikan task ini dengan sekali ketuk", Priority: "high", Complexity: "simple", Deadline: at(24)},
	} {
		if _, err := task.Create(ctx, db, id, p, pid); err != nil {
			return err
		}
	}
	return nil
}

// GuestState reports whether the account is a guest and, if so, when it expires and how many AI calls remain.
func GuestState(ctx context.Context, db *sql.DB, id int64) (isGuest bool, until time.Time, aiLeft int) {
	var used int
	var u sql.NullTime
	if err := db.QueryRowContext(ctx, `SELECT guest_until, ai_used FROM users WHERE id = $1`, id).Scan(&u, &used); err != nil || !u.Valid {
		return false, time.Time{}, 0
	}
	return true, u.Time, max(GuestAILimit-used, 0)
}

// UseAI reserves one AI call. Regular accounts are never limited here; a guest gets false once the quota is spent.
func UseAI(ctx context.Context, db *sql.DB, id int64) bool {
	var n int
	err := db.QueryRowContext(ctx, `
		SELECT CASE WHEN guest_until IS NULL THEN 1 ELSE 0 END FROM users WHERE id = $1`, id).Scan(&n)
	if err != nil {
		return false
	}
	if n == 1 {
		return true
	}
	res, err := db.ExecContext(ctx,
		`UPDATE users SET ai_used = ai_used + 1 WHERE id = $1 AND guest_until IS NOT NULL AND ai_used < $2`, id, GuestAILimit)
	if err != nil {
		return false
	}
	rows, _ := res.RowsAffected()
	return rows == 1
}

// PurgeGuests deletes expired guest accounts and, via ON DELETE CASCADE, everything they made.
func PurgeGuests(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `DELETE FROM users WHERE guest_until IS NOT NULL AND guest_until <= now() AND NOT is_admin`)
	return err
}

// ClaimGuest turns a guest into a regular sign-up, keeping its data. Like Register, the account then waits
// for admin approval, so the guest's sessions end here.
func ClaimGuest(ctx context.Context, db *sql.DB, id int64, username, email, password string) error {
	username = NormalizeUsername(username)
	email = NormalizeEmail(email)
	if err := ValidateUsername(username); err != nil {
		return err
	}
	if err := ValidateEmail(email); err != nil {
		return err
	}
	if err := ValidatePassword(password); err != nil {
		return err
	}
	if strings.EqualFold(password, username) || strings.EqualFold(password, email) {
		return ErrPasswordCommon
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	res, err := db.ExecContext(ctx, `
		UPDATE users SET username = $1, email = $2, password_hash = $3, status = 'pending', guest_until = NULL, ai_used = 0
		WHERE id = $4 AND guest_until > now() AND NOT is_admin`, username, email, string(hash), id)
	if ue := uniqueErr(err); ue != nil {
		return ue
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotGuest
	}
	_, err = db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = $1`, id)
	return err
}
