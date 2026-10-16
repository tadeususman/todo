// Package isolation checks that one user can never read or change another user's data
// through the store packages. Needs a scratch Postgres: set TEST_DATABASE_URL (skipped otherwise).
// The database is wiped of users/tasks/projects/chat rows created by this test only.
package isolation

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	"todo/internal/chat"
	appdb "todo/internal/db"
	"todo/internal/project"
	"todo/internal/task"
)

type fixture struct {
	db           *sql.DB
	alice, bob   int64
	aTask, aSub  int64
	aProj        int64
	aMsg, aProps int64
}

func setup(t *testing.T) *fixture {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	db, err := appdb.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	f := &fixture{db: db}
	tag := t.Name() + fmt.Sprint(os.Getpid())
	mk := func(name string) int64 {
		var id int64
		err := db.QueryRowContext(ctx, `INSERT INTO users (username, email, password_hash, status) VALUES ($1, $2, 'x', 'approved') RETURNING id`,
			name+"-"+tag[:8]+fmt.Sprint(os.Getpid()), name+tag+"@t.test").Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, id) }) // cascades to their data
		if err := project.EnsureDefault(ctx, db, id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	f.alice, f.bob = mk("alice"), mk("bob")

	f.aProj, err = project.Create(ctx, db, f.alice, "Alice Secret", "")
	must(t, err)
	f.aTask, err = task.Create(ctx, db, f.alice, &task.Parsed{Title: "alice private"}, f.aProj)
	must(t, err)
	f.aSub, err = task.CreateSub(ctx, db, f.alice, f.aTask, &task.Parsed{Title: "alice sub"}, f.aProj)
	must(t, err)
	f.aMsg, err = chat.Save(ctx, db, f.alice, "user", "alice chat")
	must(t, err)
	f.aProps, err = chat.SaveWithActions(ctx, db, f.alice, "assistant", "propose", []chat.Action{{TaskID: f.aTask, Field: "priority", Value: "high"}})
	must(t, err)
	return f
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestBobCannotReadAlicesData(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	if _, err := task.Get(ctx, f.db, f.bob, f.aTask); err == nil {
		t.Error("Get: bob read alice's task")
	}
	if _, err := task.Get(ctx, f.db, f.bob, f.aSub); err == nil {
		t.Error("Get: bob read alice's subtask")
	}
	if subs, _ := task.ListSubtasks(ctx, f.db, f.bob, f.aTask); len(subs) != 0 {
		t.Error("ListSubtasks leaked")
	}
	if open, _ := task.ListOpen(ctx, f.db, f.bob); len(open) != 0 {
		t.Error("ListOpen leaked")
	}
	if ps, _ := project.List(ctx, f.db, f.bob); len(ps) != 1 || ps[0].Name != project.DefaultName {
		t.Errorf("project.List leaked: %+v", ps)
	}
	if msgs, _ := chat.List(ctx, f.db, f.bob, 50); len(msgs) != 0 {
		t.Error("chat.List leaked")
	}
	if _, err := chat.Get(ctx, f.db, f.bob, f.aMsg); err == nil {
		t.Error("chat.Get: bob read alice's message")
	}
	if id, err := project.ResolveByName(ctx, f.db, f.bob, "alice secret"); err == nil && id == f.aProj {
		t.Error("ResolveByName resolved alice's project")
	}
}

func TestBobCannotModifyAlicesData(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	_ = task.Update(ctx, f.db, f.bob, f.aTask, task.UpdateFields{Title: "pwned"})
	_ = task.UpdateStatus(ctx, f.db, f.bob, f.aTask, "done")
	_ = task.SetArchived(ctx, f.db, f.bob, f.aTask, true)
	if err := task.SetProject(ctx, f.db, f.bob, f.aTask, f.aProj); err == nil {
		t.Error("SetProject: bob moved alice's task")
	}
	bobDefault, _ := project.DefaultID(ctx, f.db, f.bob)
	if err := task.SetProject(ctx, f.db, f.bob, f.aTask, bobDefault); err == nil {
		t.Error("SetProject: bob moved alice's task into his project")
	}
	if err := task.SetProject(ctx, f.db, f.alice, f.aTask, bobDefault); err == nil {
		t.Error("SetProject: alice moved her task into bob's project")
	}
	_ = project.Rename(ctx, f.db, f.bob, f.aProj, "pwned")
	_ = chat.Delete(ctx, f.db, f.bob, f.aMsg)
	if _, err := chat.ClaimActions(ctx, f.db, f.bob, f.aProps, chat.ActionApplied); err == nil {
		t.Error("ClaimActions: bob claimed alice's proposal")
	}
	if err := chat.Apply(ctx, f.db, f.bob, chat.Action{TaskID: f.aTask, Field: "priority", Value: "low"}); err == nil {
		t.Error("chat.Apply: bob changed alice's task")
	}
	if n, _ := task.DeleteMany(ctx, f.db, f.bob, []int64{f.aTask, f.aSub}); n != 0 {
		t.Error("DeleteMany removed alice's tasks")
	}
	if n, _ := task.CompleteMany(ctx, f.db, f.bob, []int64{f.aTask, f.aSub}); n != 0 {
		t.Error("CompleteMany completed alice's tasks")
	}
	if n, _ := task.ArchiveMany(ctx, f.db, f.bob, []int64{f.aTask}); n != 0 {
		t.Error("ArchiveMany touched alice's task")
	}
	_ = task.Delete(ctx, f.db, f.bob, f.aTask)
	_ = project.Delete(ctx, f.db, f.bob, f.aProj)
	_ = chat.Clear(ctx, f.db, f.bob)

	got, err := task.Get(ctx, f.db, f.alice, f.aTask)
	must(t, err)
	if got.Title != "alice private" || got.Status != "pending" || got.Priority == "low" || got.ProjectID == nil || *got.ProjectID != f.aProj {
		t.Errorf("alice's task was modified: %+v", got)
	}
	if task.IsArchived(ctx, f.db, f.alice, f.aTask) {
		t.Error("alice's task was archived by bob")
	}
	if _, err := task.Get(ctx, f.db, f.alice, f.aSub); err != nil {
		t.Error("alice's subtask vanished")
	}
	ps, _ := project.List(ctx, f.db, f.alice)
	found := false
	for _, p := range ps {
		found = found || (p.ID == f.aProj && p.Name == "Alice Secret")
	}
	if !found {
		t.Error("alice's project was renamed or deleted")
	}
	if _, err := chat.Get(ctx, f.db, f.alice, f.aMsg); err != nil {
		t.Error("alice's chat message vanished")
	}
}

// The store must not trust ids supplied by the caller: creating data that points at someone else's rows is refused.
func TestCannotAttachToOthersRows(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	if _, err := task.Create(ctx, f.db, f.bob, &task.Parsed{Title: "x"}, f.aProj); err == nil {
		t.Error("Create: bob put a task in alice's project")
	}
	if _, err := task.CreateSub(ctx, f.db, f.bob, f.aTask, &task.Parsed{Title: "x"}, 0); err == nil {
		t.Error("CreateSub: bob attached a subtask to alice's task")
	}
	subs, _ := task.ListSubtasks(ctx, f.db, f.alice, f.aTask)
	if len(subs) != 1 {
		t.Errorf("alice's task has %d subtasks, want 1", len(subs))
	}
}
