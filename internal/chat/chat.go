package chat

import (
	"context"
	"database/sql"
	"time"
)

const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

type Message struct {
	ID        int64
	UserID    int64
	Role      string
	Content   string
	CreatedAt time.Time

	// Changes the assistant proposed with this message, and where they stand (pending | applied | dismissed).
	Actions      []Action
	ActionStatus string
}

const msgCols = `id, user_id, role, content, created_at, COALESCE(actions, ''), action_status`

func scanMessage(sc interface{ Scan(...any) error }) (Message, error) {
	var m Message
	var actions string
	if err := sc.Scan(&m.ID, &m.UserID, &m.Role, &m.Content, &m.CreatedAt, &actions, &m.ActionStatus); err != nil {
		return m, err
	}
	m.Actions = decodeActions(actions)
	return m, nil
}

// List returns the most recent limit messages, oldest first.
func List(ctx context.Context, db *sql.DB, userID int64, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 100
	}
	return latest(ctx, db, `user_id = $1`, limit, userID)
}

// Search returns the most recent limit messages containing q (case-insensitive), oldest first.
func Search(ctx context.Context, db *sql.DB, userID int64, q string, limit int) ([]Message, error) {
	// strpos, not ILIKE: the user's text is matched literally, so % and _ need no escaping.
	return latest(ctx, db, `user_id = $1 AND strpos(lower(content), lower($3)) > 0`, limit, userID, q)
}

// NewerCount returns how many of the user's messages come after message id (0 if it isn't theirs).
func NewerCount(ctx context.Context, db *sql.DB, userID, id int64) (int, bool) {
	var n int
	var found bool
	err := db.QueryRowContext(ctx, `
		SELECT count(m.id), bool_or(true)
		FROM chat_messages t
		LEFT JOIN chat_messages m ON m.user_id = t.user_id AND (m.created_at, m.id) > (t.created_at, t.id)
		WHERE t.id = $1 AND t.user_id = $2`,
		id, userID).Scan(&n, &found)
	if err != nil {
		return 0, false
	}
	return n, found
}

// latest runs the newest-limit query for the given filter ($2 is the limit) and returns the rows oldest first.
func latest(ctx context.Context, db *sql.DB, where string, limit int, userID int64, extra ...any) ([]Message, error) {
	args := append([]any{userID, limit}, extra...)
	rows, err := db.QueryContext(ctx, `
		SELECT * FROM (
			SELECT `+msgCols+`
			FROM chat_messages
			WHERE `+where+`
			ORDER BY created_at DESC, id DESC
			LIMIT $2
		) latest ORDER BY created_at ASC, id ASC`,
		args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func Save(ctx context.Context, db *sql.DB, userID int64, role, content string) (int64, error) {
	var id int64
	err := db.QueryRowContext(ctx, `
		INSERT INTO chat_messages (user_id, role, content) VALUES ($1, $2, $3)
		RETURNING id`,
		userID, role, content,
	).Scan(&id)
	return id, err
}

func Delete(ctx context.Context, db *sql.DB, userID, id int64) error {
	_, err := db.ExecContext(ctx, `DELETE FROM chat_messages WHERE id = $1 AND user_id = $2`, id, userID)
	return err
}

func Clear(ctx context.Context, db *sql.DB, userID int64) error {
	_, err := db.ExecContext(ctx, `DELETE FROM chat_messages WHERE user_id = $1`, userID)
	return err
}

// Get returns a single message owned by the user.
func Get(ctx context.Context, db *sql.DB, userID, id int64) (*Message, error) {
	m, err := scanMessage(db.QueryRowContext(ctx, `
		SELECT `+msgCols+`
		FROM chat_messages WHERE id = $1 AND user_id = $2`,
		id, userID))
	if err != nil {
		return nil, err
	}
	return &m, nil
}
