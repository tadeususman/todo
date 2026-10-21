package isolation

import (
	"context"
	"strings"
	"testing"

	"todo/internal/chat"
	"todo/internal/user"
)

func TestResetRequestFlow(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	var name, email string
	must(t, f.db.QueryRowContext(ctx, `SELECT username, email FROM users WHERE id = $1`, f.alice).Scan(&name, &email))
	before := user.PendingCount(ctx, f.db)

	requested := func() bool {
		var set bool
		must(t, f.db.QueryRowContext(ctx, `SELECT reset_requested_at IS NOT NULL FROM users WHERE id = $1`, f.alice).Scan(&set))
		return set
	}

	// by username, case-insensitive
	got, ok, err := user.RequestReset(ctx, f.db, "  "+strings.ToUpper(name)+" ")
	must(t, err)
	if !ok || got != name || !requested() {
		t.Fatalf("request by username: got %q ok=%v requested=%v", got, ok, requested())
	}
	if n := user.PendingCount(ctx, f.db); n != before+1 {
		t.Fatalf("PendingCount = %d, want %d", n, before+1)
	}

	// admin reset clears it
	if _, err := user.ResetPassword(ctx, f.db, f.alice); err != nil {
		t.Fatal(err)
	}
	if requested() {
		t.Fatal("ResetPassword left the request set")
	}

	// by email, then dismissed
	if _, ok, _ := user.RequestReset(ctx, f.db, email); !ok || !requested() {
		t.Fatal("request by email not recorded")
	}
	must(t, user.DismissReset(ctx, f.db, f.alice))
	if requested() {
		t.Fatal("DismissReset left the request set")
	}

	// unknown, admin and rejected accounts are not flagged
	if _, ok, _ := user.RequestReset(ctx, f.db, "no-such-user-xyz"); ok {
		t.Fatal("unknown account reported ok")
	}
	must(t, exec(f, `UPDATE users SET is_admin = true WHERE id = $1`, f.bob))
	var bob string
	must(t, f.db.QueryRowContext(ctx, `SELECT username FROM users WHERE id = $1`, f.bob).Scan(&bob))
	if _, ok, _ := user.RequestReset(ctx, f.db, bob); ok {
		t.Fatal("admin account was flagged")
	}
	must(t, exec(f, `UPDATE users SET status = 'rejected' WHERE id = $1`, f.alice))
	if _, ok, _ := user.RequestReset(ctx, f.db, name); ok {
		t.Fatal("rejected account was flagged")
	}
}

func TestBobCannotSearchOrJumpToAlicesChat(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	msgs, err := chat.Search(ctx, f.db, f.bob, "alice chat", 100)
	must(t, err)
	if len(msgs) != 0 {
		t.Fatalf("bob's search returned %d of alice's messages", len(msgs))
	}
	if _, ok := chat.NewerCount(ctx, f.db, f.bob, f.aMsg); ok {
		t.Fatal("NewerCount accepted another user's message")
	}
	if msgs, _ := chat.Search(ctx, f.db, f.alice, "ALICE CHAT", 100); len(msgs) != 1 {
		t.Fatalf("alice's own search found %d messages, want 1", len(msgs))
	}
	if n, ok := chat.NewerCount(ctx, f.db, f.alice, f.aMsg); !ok || n != 1 {
		t.Fatalf("NewerCount for alice = %d ok=%v, want 1 true", n, ok)
	}
}

func exec(f *fixture, q string, args ...any) error {
	_, err := f.db.ExecContext(context.Background(), q, args...)
	return err
}
