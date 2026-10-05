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
}

func List(ctx context.Context, db *sql.DB, userID int64, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, user_id, role, content, created_at
		FROM chat_messages
		WHERE user_id = $1
		ORDER BY created_at ASC, id ASC
		LIMIT $2`,
		userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.UserID, &m.Role, &m.Content, &m.CreatedAt); err != nil {
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
