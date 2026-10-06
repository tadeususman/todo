package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"todo/internal/project"
	"todo/internal/task"
)

// Action statuses stored on the assistant message that proposed the actions.
const (
	ActionPending   = "pending"
	ActionApplied   = "applied"
	ActionDismissed = "dismissed"
)

// MaxActions bounds how many changes one assistant message may propose.
const MaxActions = 10

const (
	maxTitleRunes  = 200
	deadlineLayout = "2006-01-02T15:04"
)

var (
	ErrNoPending     = errors.New("tidak ada perubahan yang menunggu")
	ErrInvalidAction = errors.New("perubahan tidak valid")
)

var wib = func() *time.Location {
	if l, err := time.LoadLocation("Asia/Jakarta"); err == nil {
		return l
	}
	return time.FixedZone("WIB", 7*3600)
}()

// Action is one task change proposed by the assistant. The LLM output is untrusted:
// Prepare validates it against the user's own data before it is ever shown or stored.
type Action struct {
	TaskID int64  `json:"task_id"`
	Title  string `json:"title"` // task title snapshot, filled by the server (never trusted from the LLM)
	Field  string `json:"field"` // priority | status | deadline | project | title
	Value  string `json:"value"`
}

var statusLabel = map[string]string{
	"pending": "belum selesai", "in_progress": "sedang dikerjakan", "done": "selesai", "cancelled": "dibatalkan",
}

// Label is the human-readable description of the change, e.g. "Priority → medium".
func (a Action) Label() string {
	switch a.Field {
	case "priority":
		return "Priority → " + a.Value
	case "status":
		return "Status → " + statusLabel[a.Value]
	case "deadline":
		if a.Value == "" {
			return "Deadline → dihapus"
		}
		if t, err := time.ParseInLocation(deadlineLayout, a.Value, wib); err == nil {
			return "Deadline → " + t.Format("2 Jan 2006 15:04")
		}
	case "project":
		return "Project → " + a.Value
	case "title":
		return "Judul → " + a.Value
	}
	return a.Field + " → " + a.Value
}

// normalize validates value for field and returns its canonical form. ok is false when it is not acceptable.
func normalize(field, value string) (string, bool) {
	value = strings.TrimSpace(value)
	switch field {
	case "priority":
		value = strings.ToLower(value)
		switch value {
		case "low", "medium", "high", "urgent":
			return value, true
		}
	case "status":
		value = strings.ToLower(value)
		if _, ok := statusLabel[value]; ok {
			return value, true
		}
	case "deadline":
		if value == "" {
			return "", true // clear the deadline
		}
		for _, layout := range []string{deadlineLayout, "2006-01-02T15:04:05", "2006-01-02 15:04"} {
			if t, err := time.ParseInLocation(layout, value, wib); err == nil {
				return t.Format(deadlineLayout), true
			}
		}
		if t, err := time.Parse(time.RFC3339, value); err == nil {
			return t.In(wib).Format(deadlineLayout), true
		}
	case "project":
		if value != "" && utf8.RuneCountInString(value) <= maxTitleRunes {
			return value, true
		}
	case "title":
		if value != "" && utf8.RuneCountInString(value) <= maxTitleRunes {
			return value, true
		}
	}
	return "", false
}

// Prepare keeps only the actions that are valid for this user: the task must belong to them, the field
// must be known, and the value must be acceptable (projects must exist). Titles are overwritten from the DB.
// At most MaxActions survive, and a later action replaces an earlier one for the same task+field.
func Prepare(ctx context.Context, db *sql.DB, userID int64, in []Action) []Action {
	var out []Action
	index := map[string]int{}
	for _, a := range in {
		if len(out) >= MaxActions && index[key(a)] == 0 {
			break
		}
		v, ok := normalize(a.Field, a.Value)
		if !ok {
			continue
		}
		t, err := task.Get(ctx, db, userID, a.TaskID)
		if err != nil {
			continue
		}
		if a.Field == "project" {
			if id, err := project.ResolveByName(ctx, db, userID, v); err != nil || id == 0 {
				continue
			}
		}
		clean := Action{TaskID: t.ID, Title: t.Title, Field: a.Field, Value: v}
		if i, dup := index[key(a)]; dup {
			out[i-1] = clean
			continue
		}
		out = append(out, clean)
		index[key(a)] = len(out)
	}
	return out
}

func key(a Action) string { return strconv.FormatInt(a.TaskID, 10) + "|" + a.Field }

// Apply performs one action for the user. It re-validates everything, because stored actions are only data.
func Apply(ctx context.Context, db *sql.DB, userID int64, a Action) error {
	v, ok := normalize(a.Field, a.Value)
	if !ok {
		return ErrInvalidAction
	}
	cur, err := task.Get(ctx, db, userID, a.TaskID)
	if err != nil {
		return err
	}
	switch a.Field {
	case "status":
		return task.UpdateStatus(ctx, db, userID, cur.ID, v)
	case "project":
		id, err := project.ResolveByName(ctx, db, userID, v)
		if err != nil {
			return err
		}
		if id == 0 {
			return task.ErrProjectInvalid
		}
		return task.SetProject(ctx, db, userID, cur.ID, id)
	}
	f := task.UpdateFields{
		Title: cur.Title, Description: cur.Description, Priority: cur.Priority, Complexity: cur.Complexity,
		Deadline: cur.Deadline, EstimatedMinutes: cur.EstimatedMinutes,
	}
	switch a.Field {
	case "priority":
		f.Priority = v
	case "title":
		f.Title = v
	case "deadline":
		if v == "" {
			f.Deadline = nil
		} else {
			t, err := time.ParseInLocation(deadlineLayout, v, wib)
			if err != nil {
				return ErrInvalidAction
			}
			f.Deadline = &t
		}
	}
	return task.Update(ctx, db, userID, cur.ID, f)
}

// SaveWithActions stores an assistant message together with the changes it proposes (pending confirmation).
func SaveWithActions(ctx context.Context, db *sql.DB, userID int64, role, content string, actions []Action) (int64, error) {
	if len(actions) == 0 {
		return Save(ctx, db, userID, role, content)
	}
	raw, err := json.Marshal(actions)
	if err != nil {
		return 0, err
	}
	var id int64
	err = db.QueryRowContext(ctx, `
		INSERT INTO chat_messages (user_id, role, content, actions, action_status) VALUES ($1, $2, $3, $4, $5)
		RETURNING id`,
		userID, role, content, string(raw), ActionPending).Scan(&id)
	return id, err
}

// ClaimActions atomically moves a pending message to status (applied or dismissed) and returns its actions.
// A second call for the same message gets ErrNoPending, so a double click can't apply changes twice.
func ClaimActions(ctx context.Context, db *sql.DB, userID, id int64, status string) ([]Action, error) {
	var raw string
	err := db.QueryRowContext(ctx, `
		UPDATE chat_messages SET action_status = $3
		WHERE id = $1 AND user_id = $2 AND action_status = $4
		RETURNING COALESCE(actions, '')`,
		id, userID, status, ActionPending).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoPending
	}
	if err != nil {
		return nil, err
	}
	return decodeActions(raw), nil
}

func decodeActions(raw string) []Action {
	if raw == "" {
		return nil
	}
	var out []Action
	if json.Unmarshal([]byte(raw), &out) != nil {
		return nil
	}
	return out
}
