package user

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const resetTTL = 30 * time.Minute

var ErrResetInvalid = errors.New("Tautan reset tidak valid atau sudah kedaluwarsa.")

func hashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

// CreateResetToken issues a one-time token for the approved account with this email.
// ok=false (and no error) when there is no such account; callers must not reveal that to the requester.
// Any earlier unused tokens for the account are invalidated.
func CreateResetToken(ctx context.Context, db *sql.DB, email string) (token, username string, ok bool, err error) {
	email = NormalizeEmail(email)
	var id int64
	err = db.QueryRowContext(ctx, `SELECT id, username FROM users WHERE lower(email) = $1 AND status = 'approved'`, email).Scan(&id, &username)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", false, err
	}
	token = hex.EncodeToString(buf)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", false, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM password_resets WHERE user_id = $1`, id); err != nil {
		return "", "", false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO password_resets (token_hash, user_id, expires_at) VALUES ($1, $2, $3)`,
		hashToken(token), id, time.Now().Add(resetTTL)); err != nil {
		return "", "", false, err
	}
	return token, username, true, tx.Commit()
}

// ResetTokenValid reports whether the token exists and hasn't expired (without consuming it).
func ResetTokenValid(ctx context.Context, db *sql.DB, token string) bool {
	var n int
	_ = db.QueryRowContext(ctx, `SELECT count(*) FROM password_resets WHERE token_hash = $1 AND expires_at > now()`, hashToken(strings.TrimSpace(token))).Scan(&n)
	return n == 1
}

// ConsumeResetToken sets a new password if the token is valid, deletes the token, and ends all of the user's sessions.
// It returns the user's id.
func ConsumeResetToken(ctx context.Context, db *sql.DB, token, newPassword string) (int64, error) {
	if err := ValidatePassword(newPassword); err != nil {
		return 0, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return 0, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var id int64
	err = tx.QueryRowContext(ctx,
		`DELETE FROM password_resets WHERE token_hash = $1 AND expires_at > now() RETURNING user_id`,
		hashToken(strings.TrimSpace(token))).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrResetInvalid
	}
	if err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE users SET password_hash = $1 WHERE id = $2`, string(hash), id); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = $1`, id); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// PurgeResets removes expired reset tokens.
func PurgeResets(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `DELETE FROM password_resets WHERE expires_at <= now()`)
	return err
}
