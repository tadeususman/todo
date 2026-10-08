// Package user: akun, pendaftaran (dengan persetujuan admin), dan ganti kredensial.
package user

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"

	"todo/internal/prefs"
	"todo/internal/project"
)

const (
	StatusPending  = "pending"
	StatusApproved = "approved"
	StatusRejected = "rejected"

	MinPassword = 8
	maxPassword = 72 // batas bcrypt
)

var (
	ErrUsernameInvalid = errors.New("Username 3–32 karakter: huruf, angka, titik, strip, atau underscore.")
	ErrUsernameTaken   = errors.New("Username sudah dipakai.")
	ErrEmailInvalid    = errors.New("Format email tidak valid.")
	ErrEmailTaken      = errors.New("Email sudah terdaftar.")
	ErrPasswordShort   = errors.New("Password minimal 8 karakter.")
	ErrPasswordLong    = errors.New("Password maksimal 72 karakter.")
	ErrPasswordWrong   = errors.New("Password saat ini salah.")
	ErrNotFound        = errors.New("user tidak ditemukan")
)

// dummyHash dipakai untuk menyamakan waktu respons saat username tidak ada.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("dummy-password"), bcrypt.DefaultCost)

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{2,31}$`)

// NormalizeUsername trims whitespace. Huruf besar/kecil dipertahankan untuk tampilan;
// keunikan dan login tidak peduli besar-kecil (index lower(username) + lower() saat login).
func NormalizeUsername(s string) string { return strings.TrimSpace(s) }

func ValidateUsername(s string) error {
	if !usernameRe.MatchString(s) {
		return ErrUsernameInvalid
	}
	return nil
}

// NormalizeEmail trims and lowercases (email dianggap tidak peduli besar-kecil).
func NormalizeEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func ValidateEmail(s string) error {
	if s == "" || len(s) > 254 {
		return ErrEmailInvalid
	}
	a, err := mail.ParseAddress(s)
	// ParseAddress menerima "Nama <a@b.c>"; kita hanya mau alamat polos.
	if err != nil || a.Address != s || !strings.Contains(s[strings.LastIndex(s, "@"):], ".") {
		return ErrEmailInvalid
	}
	return nil
}

func ValidatePassword(s string) error {
	if len(s) < MinPassword {
		return ErrPasswordShort
	}
	if len(s) > maxPassword {
		return ErrPasswordLong
	}
	return nil
}

type User struct {
	ID        int64
	Username  string
	Email     string
	Status    string
	IsAdmin   bool
	CreatedAt time.Time
}

// uniqueErr maps a unique-violation to ErrUsernameTaken / ErrEmailTaken (nil if err isn't one).
func uniqueErr(err error) error {
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) || pqErr.Code != "23505" {
		return nil
	}
	if strings.Contains(pqErr.Constraint, "email") {
		return ErrEmailTaken
	}
	return ErrUsernameTaken
}

// Register creates a user with status 'pending'. They cannot log in until an admin approves.
func Register(ctx context.Context, db *sql.DB, username, email, password string) error {
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
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	var id int64
	err = db.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, status) VALUES ($1, $2, $3, 'pending') RETURNING id`,
		username, email, string(hash)).Scan(&id)
	if ue := uniqueErr(err); ue != nil {
		return ue
	}
	if err != nil {
		return err
	}
	// Siapkan data awal supaya begitu disetujui user langsung bisa dipakai.
	if err := project.EnsureDefault(ctx, db, id); err != nil {
		return err
	}
	return prefs.EnsureRow(ctx, db, id)
}

// Authenticate checks the password and returns the user (with status) on success.
func Authenticate(ctx context.Context, db *sql.DB, username, password string) (*User, error) {
	var u User
	var hash string
	err := db.QueryRowContext(ctx,
		`SELECT id, username, status, is_admin, password_hash FROM users
		 WHERE lower(username) = lower($1) OR lower(email) = lower($1)`,
		strings.TrimSpace(username)).Scan(&u.ID, &u.Username, &u.Status, &u.IsAdmin, &hash)
	if err != nil {
		// samakan biaya waktu agar username yang ada/tidak ada tidak mudah dibedakan
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return nil, ErrNotFound
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return nil, ErrNotFound
	}
	return &u, nil
}

func Get(ctx context.Context, db *sql.DB, id int64) (*User, error) {
	var u User
	err := db.QueryRowContext(ctx,
		`SELECT id, username, coalesce(email, ''), status, is_admin, created_at FROM users WHERE id = $1`, id).
		Scan(&u.ID, &u.Username, &u.Email, &u.Status, &u.IsAdmin, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &u, err
}

func IsAdmin(ctx context.Context, db *sql.DB, id int64) bool {
	var ok bool
	_ = db.QueryRowContext(ctx, `SELECT is_admin FROM users WHERE id = $1 AND status = 'approved'`, id).Scan(&ok)
	return ok
}

func Rename(ctx context.Context, db *sql.DB, id int64, newName string) error {
	newName = NormalizeUsername(newName)
	if err := ValidateUsername(newName); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `UPDATE users SET username = $1 WHERE id = $2`, newName, id)
	if ue := uniqueErr(err); ue != nil {
		return ue
	}
	return err
}

func SetEmail(ctx context.Context, db *sql.DB, id int64, email string) error {
	email = NormalizeEmail(email)
	if err := ValidateEmail(email); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `UPDATE users SET email = $1 WHERE id = $2`, email, id)
	if ue := uniqueErr(err); ue != nil {
		return ue
	}
	return err
}

func ChangePassword(ctx context.Context, db *sql.DB, id int64, current, next string) error {
	if err := ValidatePassword(next); err != nil {
		return err
	}
	var hash string
	if err := db.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id = $1`, id).Scan(&hash); err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(current)) != nil {
		return ErrPasswordWrong
	}
	nh, err := bcrypt.GenerateFromPassword([]byte(next), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `UPDATE users SET password_hash = $1 WHERE id = $2`, string(nh), id)
	return err
}

const tempPasswordAlphabet = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// ResetPassword (oleh admin) mengganti password user non-admin dengan password sementara acak,
// memutus semua sesinya, dan mengembalikan password itu sekali saja.
func ResetPassword(ctx context.Context, db *sql.DB, id int64) (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	pw := make([]byte, len(buf))
	for i, b := range buf {
		pw[i] = tempPasswordAlphabet[int(b)%len(tempPasswordAlphabet)]
	}
	h, err := bcrypt.GenerateFromPassword(pw, bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	res, err := db.ExecContext(ctx, `UPDATE users SET password_hash = $1 WHERE id = $2 AND is_admin = false`, string(h), id)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", ErrNotFound
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = $1`, id); err != nil {
		return "", err
	}
	return string(pw), nil
}

// List returns all users, pending first, then newest.
func List(ctx context.Context, db *sql.DB) ([]User, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, username, coalesce(email, ''), status, is_admin, created_at FROM users
		ORDER BY (status = 'pending') DESC, created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.Status, &u.IsAdmin, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func PendingCount(ctx context.Context, db *sql.DB) int {
	var n int
	_ = db.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE status = 'pending'`).Scan(&n)
	return n
}

// SetStatus approves or rejects a non-admin user. Rejecting also ends their sessions.
func SetStatus(ctx context.Context, db *sql.DB, id int64, status string) error {
	if status != StatusApproved && status != StatusRejected {
		return errors.New("status tidak valid")
	}
	res, err := db.ExecContext(ctx, `UPDATE users SET status = $1 WHERE id = $2 AND is_admin = false`, status, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if status == StatusRejected {
		_, err = db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = $1`, id)
	}
	return err
}
