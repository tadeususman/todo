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
	rows, err := db.QueryContext(ctx, `
		SELECT * FROM (
			SELECT `+msgCols+`
			FROM chat_messages
			WHERE user_id = $1
			ORDER BY created_at DESC, id DESC
			LIMIT $2
		) latest ORDER BY created_at ASC, id ASC`,
		userID, limit)
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
