package task

import (
	"context"
	"database/sql"
	"time"
)

type Task struct {
	ID               int64
	UserID           int64
	ParentID         *int64
	ProjectID        *int64
	ProjectName      string
	ProjectColor     string
	Title            string
	Description      string
	Status           string
	Priority         string
	Complexity       string
	Deadline         *time.Time
	EstimatedMinutes *int
	CompletedAt      *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Parsed is the LLM-extracted task shape returned by llm.ParseTask.
type Parsed struct {
	Title            string     `json:"title"`
	Description      string     `json:"description"`
	Priority         string     `json:"priority"`   // low | medium | high | urgent
	Complexity       string     `json:"complexity"` // simple | medium | complex
	Deadline         *time.Time `json:"deadline,omitempty"`
	EstimatedMinutes *int       `json:"estimated_minutes,omitempty"`
	Project          string     `json:"project,omitempty"` // project name (must match an existing project; empty → default)
}

const selectCols = `
    t.id, t.user_id, t.parent_id, t.project_id,
    COALESCE(p.name, ''), COALESCE(p.color, ''),
    t.title, t.description, t.status, t.priority, t.complexity,
    t.deadline, t.estimated_minutes, t.completed_at, t.created_at, t.updated_at`

func Create(ctx context.Context, db *sql.DB, userID int64, p *Parsed, projectID int64) (int64, error) {
	var id int64
	err := db.QueryRowContext(ctx, `
		INSERT INTO tasks (user_id, project_id, title, description, priority, complexity, deadline, estimated_minutes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id`,
		userID, projectID, p.Title, p.Description, defaultStr(p.Priority, "medium"),
		defaultStr(p.Complexity, "simple"), p.Deadline, p.EstimatedMinutes,
	).Scan(&id)
	return id, err
}

// CreateSub inserts a task that lives under a parent task, inheriting its project.
func CreateSub(ctx context.Context, db *sql.DB, userID, parentID int64, p *Parsed, projectID int64) (int64, error) {
	var id int64
	err := db.QueryRowContext(ctx, `
		INSERT INTO tasks (user_id, parent_id, project_id, title, description, priority, complexity, deadline, estimated_minutes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id`,
		userID, parentID, projectID, p.Title, p.Description, defaultStr(p.Priority, "medium"),
		defaultStr(p.Complexity, "simple"), p.Deadline, p.EstimatedMinutes,
	).Scan(&id)
	return id, err
}

func Get(ctx context.Context, db *sql.DB, userID, id int64) (*Task, error) {
	var t Task
	err := db.QueryRowContext(ctx, `
		SELECT `+selectCols+`
		FROM tasks t LEFT JOIN projects p ON p.id = t.project_id
		WHERE t.id = $1 AND t.user_id = $2`,
		id, userID,
	).Scan(&t.ID, &t.UserID, &t.ParentID, &t.ProjectID, &t.ProjectName, &t.ProjectColor,
		&t.Title, &t.Description, &t.Status, &t.Priority, &t.Complexity, &t.Deadline,
		&t.EstimatedMinutes, &t.CompletedAt, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ListOpen returns pending + in_progress tasks (no subtasks) ordered by deadline.
func ListOpen(ctx context.Context, db *sql.DB, userID int64) ([]Task, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT `+selectCols+`
		FROM tasks t LEFT JOIN projects p ON p.id = t.project_id
		WHERE t.user_id = $1 AND t.status IN ('pending','in_progress') AND t.parent_id IS NULL
		ORDER BY
		  CASE WHEN t.deadline IS NULL THEN 1 ELSE 0 END,
		  t.deadline ASC,
		  CASE t.priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 ELSE 3 END`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTasks(rows)
}

// ListRecentDone returns the most recently completed top-level tasks (no subtasks).
func ListRecentDone(ctx context.Context, db *sql.DB, userID int64, limit int) ([]Task, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := db.QueryContext(ctx, `
		SELECT `+selectCols+`
		FROM tasks t LEFT JOIN projects p ON p.id = t.project_id
		WHERE t.user_id = $1 AND t.status = 'done' AND t.parent_id IS NULL
		ORDER BY t.completed_at DESC NULLS LAST
		LIMIT $2`,
		userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTasks(rows)
}

func ListSubtasks(ctx context.Context, db *sql.DB, userID, parentID int64) ([]Task, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT `+selectCols+`
		FROM tasks t LEFT JOIN projects p ON p.id = t.project_id
		WHERE t.user_id = $1 AND t.parent_id = $2
		ORDER BY t.created_at ASC`,
		userID, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTasks(rows)
}

// Update mutates the editable fields of a task.
type UpdateFields struct {
	Title            string
	Description      string
	Priority         string
	Complexity       string
	Deadline         *time.Time
	EstimatedMinutes *int
}

func Update(ctx context.Context, db *sql.DB, userID, id int64, f UpdateFields) error {
	_, err := db.ExecContext(ctx, `
		UPDATE tasks
		SET title = $1,
		    description = $2,
		    priority = $3,
		    complexity = $4,
		    deadline = $5,
		    estimated_minutes = $6,
		    updated_at = now()
		WHERE id = $7 AND user_id = $8`,
		f.Title, f.Description,
		defaultStr(f.Priority, "medium"),
		defaultStr(f.Complexity, "simple"),
		f.Deadline, f.EstimatedMinutes,
		id, userID)
	return err
}

func UpdateStatus(ctx context.Context, db *sql.DB, userID, id int64, status string) error {
	if !validStatus(status) {
		return nil
	}
	completed := sql.NullTime{}
	if status == "done" {
		completed = sql.NullTime{Time: time.Now(), Valid: true}
	}
	_, err := db.ExecContext(ctx, `
		UPDATE tasks SET status = $1, completed_at = $2, updated_at = now()
		WHERE id = $3 AND user_id = $4`,
		status, completed, id, userID)
	return err
}

// SetProject assigns a task to a project.
func SetProject(ctx context.Context, db *sql.DB, userID, id, projectID int64) error {
	_, err := db.ExecContext(ctx, `
		UPDATE tasks SET project_id = $1, updated_at = now()
		WHERE id = $2 AND user_id = $3`,
		projectID, id, userID)
	return err
}

func Delete(ctx context.Context, db *sql.DB, userID, id int64) error {
	_, err := db.ExecContext(ctx, `DELETE FROM tasks WHERE id = $1 AND user_id = $2`, id, userID)
	return err
}

func scanTasks(rows *sql.Rows) ([]Task, error) {
	var out []Task
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.UserID, &t.ParentID, &t.ProjectID, &t.ProjectName, &t.ProjectColor,
			&t.Title, &t.Description, &t.Status, &t.Priority, &t.Complexity, &t.Deadline,
			&t.EstimatedMinutes, &t.CompletedAt, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func validStatus(s string) bool {
	switch s {
	case "pending", "in_progress", "done", "cancelled":
		return true
	}
	return false
}

func defaultStr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
