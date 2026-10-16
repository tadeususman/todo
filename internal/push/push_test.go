package push

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	appdb "todo/internal/db"
	"todo/internal/task"
)

func TestValidEndpoint(t *testing.T) {
	ok := []string{
		"https://fcm.googleapis.com/fcm/send/abc",
		"https://updates.push.services.mozilla.com/wpush/v2/abc",
		"https://web.push.apple.com/abc",
		"https://wns2-par02p.notify.windows.com/?token=x",
	}
	bad := []string{
		"http://fcm.googleapis.com/x", "https://evil.example/x", "https://fcm.googleapis.com.evil.example/x",
		"https://169.254.169.254/latest/meta-data", "https://localhost/x", "https://fcm.googleapis.com:8443/x",
		"https://user@fcm.googleapis.com/x", "ftp://fcm.googleapis.com/x", "", "https://notfcm.googleapis.com.attacker.io/",
		"https://" + strings.Repeat("a", 3000) + ".push.apple.com/",
	}
	for _, u := range ok {
		if !validEndpoint(u) {
			t.Errorf("should accept %s", u)
		}
	}
	for _, u := range bad {
		if validEndpoint(u) {
			t.Errorf("should reject %.60s", u)
		}
	}
}

type stubTransport struct{ n *atomic.Int32 }

func (s stubTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	s.n.Add(1)
	return &http.Response{StatusCode: 201, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
}

func newKey(t *testing.T) (p256dh, auth string) {
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	a := make([]byte, 16)
	rand.Read(a)
	return base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()), base64.RawURLEncoding.EncodeToString(a)
}

func TestRemindDue(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	db, err := appdb.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var n atomic.Int32
	svc, err := New(ctx, db, "mailto:test@example.com")
	if err != nil {
		t.Fatal(err)
	}
	svc.client = &http.Client{Transport: stubTransport{&n}}

	mkUser := func(name string) int64 {
		var id int64
		must(t, db.QueryRow(`INSERT INTO users (username, email, password_hash, status) VALUES ($1, $2, 'x', 'approved') RETURNING id`,
			fmt.Sprintf("%s-%d", name, os.Getpid()), fmt.Sprintf("%s-%d@t.test", name, os.Getpid())).Scan(&id))
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })
		_, err := db.Exec(`INSERT INTO user_prefs (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, id)
		must(t, err)
		return id
	}
	mkTask := func(uid int64, title, status string, minsAhead int) int64 {
		var id int64
		must(t, db.QueryRow(`INSERT INTO tasks (user_id, title, status, deadline) VALUES ($1, $2, $3, now() + make_interval(mins => $4)) RETURNING id`,
			uid, title, status, minsAhead).Scan(&id))
		return id
	}
	reminded := func(id int64) bool {
		var ok bool
		must(t, db.QueryRow(`SELECT reminded_at IS NOT NULL FROM tasks WHERE id = $1`, id).Scan(&ok))
		return ok
	}

	withDev, noDev := mkUser("withdev"), mkUser("nodev")
	p, a := newKey(t)
	must(t, svc.Subscribe(ctx, withDev, "https://fcm.googleapis.com/fcm/send/test-"+fmt.Sprint(os.Getpid()), p, a))

	soon := mkTask(withDev, "soon", "pending", 30)
	far := mkTask(withDev, "far", "pending", 600)
	done := mkTask(withDev, "done", "done", 30)
	past := mkTask(withDev, "past", "pending", -30)
	nosub := mkTask(noDev, "nosub", "pending", 30)

	must(t, svc.RemindDue(ctx))
	if got := n.Load(); got != 1 {
		t.Fatalf("sent %d pushes, want 1", got)
	}
	if !reminded(soon) || reminded(far) || reminded(done) || reminded(past) || reminded(nosub) {
		t.Errorf("wrong tasks claimed: soon=%v far=%v done=%v past=%v nosub=%v", reminded(soon), reminded(far), reminded(done), reminded(past), reminded(nosub))
	}

	must(t, svc.RemindDue(ctx))
	if n.Load() != 1 {
		t.Error("reminder repeated for the same task")
	}

	// Moving the deadline re-arms the reminder; saving the same deadline doesn't.
	cur, err := task.Get(ctx, db, withDev, soon)
	must(t, err)
	f := task.UpdateFields{Title: "soon edited", Priority: "medium", Complexity: "simple", Deadline: cur.Deadline}
	must(t, task.Update(ctx, db, withDev, soon, f))
	if !reminded(soon) {
		t.Error("editing the title re-armed the reminder")
	}
	later := cur.Deadline.Add(10 * 60 * 1e9)
	f.Deadline = &later
	must(t, task.Update(ctx, db, withDev, soon, f))
	if reminded(soon) {
		t.Error("changing the deadline did not re-arm the reminder")
	}
	n.Store(1)
	must(t, db.QueryRow(`UPDATE tasks SET reminded_at = now() WHERE id = $1 RETURNING true`, soon).Scan(new(bool)))

	// Lead time 0 switches reminders off.
	must(t, SetLead(ctx, db, withDev, 0))
	mkTask(withDev, "soon2", "pending", 10)
	must(t, svc.RemindDue(ctx))
	if n.Load() != 1 {
		t.Error("reminder sent although the user turned them off")
	}
	must(t, SetLead(ctx, db, withDev, 1440))
	must(t, svc.RemindDue(ctx))
	if n.Load() != 3 { // soon2 + far (600 min < 1 day)
		t.Errorf("after widening lead time sent %d, want 3", n.Load())
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
