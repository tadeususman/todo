package project

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Project struct {
	ID        int64
	UserID    int64
	Name      string
	Color     string
	IsDefault bool
	CreatedAt time.Time
}

const DefaultName = "General"

var ErrNameEmpty = errors.New("project name empty")
var ErrDeleteDefault = errors.New("cannot delete default project")

// EnsureDefault inserts the "General" project for the user if missing.
// Idempotent; safe to call on every startup.
func EnsureDefault(ctx context.Context, db *sql.DB, userID int64) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO projects (user_id, name, color, is_default)
		VALUES ($1, $2, '#64748b', true)
		ON CONFLICT (user_id, name) DO UPDATE SET is_default = true`,
		userID, DefaultName)
	return err
}

func List(ctx context.Context, db *sql.DB, userID int64) ([]Project, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, user_id, name, color, is_default, created_at
		FROM projects
		WHERE user_id = $1
		ORDER BY is_default DESC, name ASC`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.UserID, &p.Name, &p.Color, &p.IsDefault, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func Create(ctx context.Context, db *sql.DB, userID int64, name, color string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, ErrNameEmpty
	}
	if color == "" {
		color = "#64748b"
	}
	var id int64
	err := db.QueryRowContext(ctx, `
		INSERT INTO projects (user_id, name, color) VALUES ($1, $2, $3)
		ON CONFLICT (user_id, name) DO UPDATE SET color = EXCLUDED.color
		RETURNING id`,
		userID, name, color,
	).Scan(&id)
	return id, err
}

func Rename(ctx context.Context, db *sql.DB, userID, id int64, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrNameEmpty
	}
	_, err := db.ExecContext(ctx, `
		UPDATE projects SET name = $1
		WHERE id = $2 AND user_id = $3 AND is_default = false`,
		name, id, userID)
	return err
}

// Delete removes a non-default project and reassigns its tasks to the user's default project.
func Delete(ctx context.Context, db *sql.DB, userID, id int64) error {
	var isDefault bool
	err := db.QueryRowContext(ctx, `SELECT is_default FROM projects WHERE id = $1 AND user_id = $2`, id, userID).Scan(&isDefault)
	if err != nil {
		return err
	}
	if isDefault {
		return ErrDeleteDefault
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var defaultID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM projects WHERE user_id = $1 AND is_default = true`, userID).Scan(&defaultID); err != nil {
		return fmt.Errorf("locate default project: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET project_id = $1 WHERE user_id = $2 AND project_id = $3`, defaultID, userID, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM projects WHERE id = $1 AND user_id = $2`, id, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// DefaultID returns the user's default project id.
func DefaultID(ctx context.Context, db *sql.DB, userID int64) (int64, error) {
	var id int64
	err := db.QueryRowContext(ctx, `SELECT id FROM projects WHERE user_id = $1 AND is_default = true`, userID).Scan(&id)
	return id, err
}

// ResolveByName finds a project by case-insensitive name for the user. Returns 0 if not found.
func ResolveByName(ctx context.Context, db *sql.DB, userID int64, name string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, nil
	}
	var id int64
	err := db.QueryRowContext(ctx, `SELECT id FROM projects WHERE user_id = $1 AND lower(name) = lower($2)`, userID, name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}
