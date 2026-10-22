package isolation

import (
	"context"
	"testing"

	"todo/internal/task"
	"todo/internal/user"
)

func TestGuestLifecycle(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	id, err := user.CreateGuest(ctx, f.db)
	must(t, err)
	guest, until, left := user.GuestState(ctx, f.db, id)
	if !guest || until.IsZero() || left != user.GuestAILimit {
		t.Fatalf("guest state: %v %v %d", guest, until, left)
	}
	if g, _, _ := user.GuestState(ctx, f.db, f.alice); g {
		t.Error("regular user reported as guest")
	}
	var n int
	must(t, f.db.QueryRow(`SELECT count(*) FROM tasks WHERE user_id = $1`, id).Scan(&n))
	if n == 0 {
		t.Error("guest has no sample tasks")
	}

	// Quota: guests are capped, regular accounts are not.
	for i := 0; i < user.GuestAILimit; i++ {
		if !user.UseAI(ctx, f.db, id) {
			t.Fatalf("guest denied AI call %d", i+1)
		}
	}
	if user.UseAI(ctx, f.db, id) {
		t.Error("guest exceeded AI quota")
	}
	if !user.UseAI(ctx, f.db, f.alice) {
		t.Error("regular user limited")
	}

	// Guests are hidden from the admin list and cannot log in with a password.
	users, err := user.List(ctx, f.db)
	must(t, err)
	for _, u := range users {
		if u.ID == id {
			t.Error("guest listed for admin")
		}
	}
	if _, err := user.Authenticate(ctx, f.db, "tamu", "!"); err == nil {
		t.Error("guest authenticated by password")
	}

	// Expired guest: session stops working and purge removes the data.
	_, err = f.db.Exec(`INSERT INTO sessions (id, user_id, expires_at) VALUES ('guest-sess', $1, now() + interval '1 day')`, id)
	must(t, err)
	_, err = f.db.Exec(`UPDATE users SET guest_until = now() - interval '1 minute' WHERE id = $1`, id)
	must(t, err)
	if err := user.ClaimGuest(ctx, f.db, id, "claimed-"+t.Name(), "c@t.test", "kopi-susu-pagi-9"); err != user.ErrNotGuest {
		t.Errorf("expired guest claimed: %v", err)
	}
	must(t, user.PurgeGuests(ctx, f.db))
	must(t, f.db.QueryRow(`SELECT count(*) FROM users WHERE id = $1`, id).Scan(&n))
	if n != 0 {
		t.Error("expired guest not purged")
	}
}

func TestGuestClaim(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	id, err := user.CreateGuest(ctx, f.db)
	must(t, err)
	if err := user.ClaimGuest(ctx, f.db, id, "claim"+t.Name(), "claim@t.test", "password123"); err != user.ErrPasswordCommon {
		t.Errorf("weak password accepted: %v", err)
	}
	name := "claim" + t.Name()
	must(t, user.ClaimGuest(ctx, f.db, id, name, "claim@t.test", "kopi-susu-pagi-9"))
	u, err := user.Get(ctx, f.db, id)
	must(t, err)
	if u.Status != user.StatusPending || u.Username != name {
		t.Errorf("after claim: %+v", u)
	}
	if g, _, _ := user.GuestState(ctx, f.db, id); g {
		t.Error("still a guest after claim")
	}
	var n int
	must(t, f.db.QueryRow(`SELECT count(*) FROM tasks WHERE user_id = $1`, id).Scan(&n))
	if n == 0 {
		t.Error("claim lost the guest's tasks")
	}
	if err := user.ClaimGuest(ctx, f.db, id, name, "claim@t.test", "kopi-susu-pagi-9"); err != user.ErrNotGuest {
		t.Errorf("second claim: %v", err)
	}
	must(t, user.PurgeGuests(ctx, f.db))
}

func TestTaskUnseenUntilOpened(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	id, err := task.Create(ctx, f.db, f.alice, &task.Parsed{Title: "baru"}, 0)
	must(t, err)
	got, err := task.Get(ctx, f.db, f.alice, id)
	must(t, err)
	if !got.Unseen {
		t.Fatal("new task not marked unseen")
	}
	must(t, task.MarkSeen(ctx, f.db, f.bob, id)) // someone else opening it changes nothing
	if got, _ = task.Get(ctx, f.db, f.alice, id); !got.Unseen {
		t.Error("another user cleared the mark")
	}
	must(t, task.MarkSeen(ctx, f.db, f.alice, id))
	if got, _ = task.Get(ctx, f.db, f.alice, id); got.Unseen {
		t.Error("mark not cleared by owner")
	}
}
