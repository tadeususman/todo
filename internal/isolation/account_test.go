package isolation

import (
	"context"
	"fmt"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"todo/internal/task"
	"todo/internal/user"
)

func setPassword(t *testing.T, f *fixture, id int64, pw string) {
	t.Helper()
	h, _ := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.MinCost)
	_, err := f.db.Exec(`UPDATE users SET password_hash = $1 WHERE id = $2`, string(h), id)
	must(t, err)
}

func TestPasswordResetFlow(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	email := "reset-" + t.Name() + "@t.test"
	_, err := f.db.Exec(`UPDATE users SET email = $1 WHERE id = $2`, email, f.alice)
	must(t, err)
	_, err = f.db.Exec(`INSERT INTO sessions (id, user_id, expires_at) VALUES ($1, $2, now() + interval '1 day')`, fmt.Sprintf("test-session-%d", f.alice), f.alice)
	must(t, err)

	if _, _, ok, err := user.CreateResetToken(ctx, f.db, "nobody@t.test"); ok || err != nil {
		t.Fatalf("unknown email must be silently ignored: ok=%v err=%v", ok, err)
	}
	tok, _, ok, err := user.CreateResetToken(ctx, f.db, email)
	if !ok || err != nil {
		t.Fatalf("token: ok=%v err=%v", ok, err)
	}
	var stored string
	must(t, f.db.QueryRow(`SELECT token_hash FROM password_resets WHERE user_id = $1`, f.alice).Scan(&stored))
	if stored == tok {
		t.Error("raw token stored in database")
	}
	// A newer request invalidates the older token.
	tok2, _, _, _ := user.CreateResetToken(ctx, f.db, email)
	if user.ResetTokenValid(ctx, f.db, tok) {
		t.Error("old token still valid after a new request")
	}
	if _, err := user.ConsumeResetToken(ctx, f.db, tok2, "password123"); err != user.ErrPasswordCommon {
		t.Errorf("weak password accepted on reset: %v", err)
	}
	if !user.ResetTokenValid(ctx, f.db, tok2) {
		t.Error("token burned by a rejected password")
	}
	id, err := user.ConsumeResetToken(ctx, f.db, tok2, "kopi-susu-pagi-9")
	if err != nil || id != f.alice {
		t.Fatalf("consume: id=%d err=%v", id, err)
	}
	if _, err := user.ConsumeResetToken(ctx, f.db, tok2, "another-good-pass-1"); err != user.ErrResetInvalid {
		t.Errorf("token reusable: %v", err)
	}
	var n int
	must(t, f.db.QueryRow(`SELECT count(*) FROM sessions WHERE user_id = $1`, f.alice).Scan(&n))
	if n != 0 {
		t.Error("sessions survived password reset")
	}
	if _, err := user.Authenticate(ctx, f.db, "x", "kopi-susu-pagi-9"); err == nil {
		t.Log("(sanity) wrong username must not authenticate")
	}

	// Expired token.
	tok3, _, _, _ := user.CreateResetToken(ctx, f.db, email)
	_, err = f.db.Exec(`UPDATE password_resets SET expires_at = now() - interval '1 minute' WHERE user_id = $1`, f.alice)
	must(t, err)
	if _, err := user.ConsumeResetToken(ctx, f.db, tok3, "kopi-susu-pagi-9"); err != user.ErrResetInvalid {
		t.Errorf("expired token accepted: %v", err)
	}

	// Pending accounts can't be reset into existence.
	_, err = f.db.Exec(`UPDATE users SET status = 'pending' WHERE id = $1`, f.alice)
	must(t, err)
	if _, _, ok, _ := user.CreateResetToken(ctx, f.db, email); ok {
		t.Error("reset token issued for a pending account")
	}
}

func TestDeleteAccount(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	setPassword(t, f, f.alice, "kopi-susu-pagi-9")

	if err := user.DeleteAccount(ctx, f.db, f.alice, "wrong"); err != user.ErrPasswordWrong {
		t.Fatalf("wrong password: %v", err)
	}
	if _, err := task.Get(ctx, f.db, f.alice, f.aTask); err != nil {
		t.Fatal("data deleted despite wrong password")
	}
	must(t, user.DeleteAccount(ctx, f.db, f.alice, "kopi-susu-pagi-9"))

	for _, tbl := range []string{"tasks", "projects", "chat_messages", "user_prefs", "sessions"} {
		var n int
		must(t, f.db.QueryRow(`SELECT count(*) FROM `+tbl+` WHERE user_id = $1`, f.alice).Scan(&n))
		if n != 0 {
			t.Errorf("%s: %d rows left after account deletion", tbl, n)
		}
	}
	// Bob is untouched.
	if _, err := user.Get(ctx, f.db, f.bob); err != nil {
		t.Error("bob vanished")
	}
}

func TestLastAdminCannotDelete(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	// Make sure alice is the only approved admin for the duration of the test.
	var others int
	err := f.db.QueryRow(`SELECT count(*) FROM users WHERE is_admin AND status = 'approved'`).Scan(&others)
	must(t, err)
	if others > 0 {
		t.Skip("database already has admins; run against a scratch database")
	}
	setPassword(t, f, f.alice, "kopi-susu-pagi-9")
	_, err = f.db.Exec(`UPDATE users SET is_admin = true WHERE id = $1`, f.alice)
	must(t, err)
	if err := user.DeleteAccount(ctx, f.db, f.alice, "kopi-susu-pagi-9"); err != user.ErrLastAdmin {
		t.Errorf("last admin deleted: %v", err)
	}
	_, err = f.db.Exec(`UPDATE users SET is_admin = true WHERE id = $1`, f.bob)
	must(t, err)
	if err := user.DeleteAccount(ctx, f.db, f.alice, "kopi-susu-pagi-9"); err != nil {
		t.Errorf("admin with a co-admin should be deletable: %v", err)
	}
}
