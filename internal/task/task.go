package task

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/lib/pq"
)

var (
	ErrInvalidStatus  = errors.New("status tidak valid")
	ErrProjectInvalid = errors.New("project tidak ditemukan")
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
	IsTask           *bool      `json:"is_task,omitempty"`       // false → input bukan task/jadwal; nil dianggap true
	RejectReason     string     `json:"reject_reason,omitempty"` // pesan penolakan halus kalau is_task=false
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
		userID, projectID, p.Title, p.Description, NormPriority(p.Priority),
		NormComplexity(p.Complexity), p.Deadline, p.EstimatedMinutes,
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
		userID, parentID, projectID, p.Title, p.Description, NormPriority(p.Priority),
		NormComplexity(p.Complexity), p.Deadline, p.EstimatedMinutes,
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
		WHERE t.user_id = $1 AND t.status IN ('pending','in_progress') AND t.parent_id IS NULL AND t.archived_at IS NULL
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
		WHERE t.user_id = $1 AND t.status = 'done' AND t.parent_id IS NULL AND t.archived_at IS NULL
		ORDER BY t.completed_at DESC NULLS LAST
		LIMIT $2`,
		userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTasks(rows)
}

// ListDoneBetween returns top-level tasks completed within [from, to), newest first.
func ListDoneBetween(ctx context.Context, db *sql.DB, userID int64, from, to time.Time) ([]Task, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT `+selectCols+`
		FROM tasks t LEFT JOIN projects p ON p.id = t.project_id
		WHERE t.user_id = $1 AND t.status = 'done' AND t.parent_id IS NULL
		  AND t.archived_at IS NULL AND t.completed_at >= $2 AND t.completed_at < $3
		ORDER BY t.completed_at DESC`,
		userID, from, to)
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
		NormPriority(f.Priority),
		NormComplexity(f.Complexity),
		f.Deadline, f.EstimatedMinutes,
		id, userID)
	return err
}

func UpdateStatus(ctx context.Context, db *sql.DB, userID, id int64, status string) error {
	if !validStatus(status) {
		return ErrInvalidStatus
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
	res, err := db.ExecContext(ctx, `
		UPDATE tasks SET project_id = $1, updated_at = now()
		WHERE id = $2 AND user_id = $3
		  AND EXISTS (SELECT 1 FROM projects WHERE id = $1 AND user_id = $3)`,
		projectID, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrProjectInvalid
	}
	return nil
}

// SetArchived hides (or restores) a top-level task. Its status is left untouched.
func SetArchived(ctx context.Context, db *sql.DB, userID, id int64, archived bool) error {
	_, err := db.ExecContext(ctx, `
		UPDATE tasks
		SET archived_at = CASE WHEN $3 THEN now() ELSE NULL END, updated_at = now()
		WHERE id = $1 AND user_id = $2 AND parent_id IS NULL`,
		id, userID, archived)
	return err
}

func IsArchived(ctx context.Context, db *sql.DB, userID, id int64) bool {
	var ok bool
	_ = db.QueryRowContext(ctx, `SELECT archived_at IS NOT NULL FROM tasks WHERE id = $1 AND user_id = $2`, id, userID).Scan(&ok)
	return ok
}

// ListArchived returns archived top-level tasks, most recently archived first.
func ListArchived(ctx context.Context, db *sql.DB, userID int64) ([]Task, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT `+selectCols+`
		FROM tasks t LEFT JOIN projects p ON p.id = t.project_id
		WHERE t.user_id = $1 AND t.parent_id IS NULL AND t.archived_at IS NOT NULL
		ORDER BY t.archived_at DESC`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTasks(rows)
}

func CountArchived(ctx context.Context, db *sql.DB, userID int64) int {
	var n int
	_ = db.QueryRowContext(ctx, `SELECT count(*) FROM tasks WHERE user_id = $1 AND parent_id IS NULL AND archived_at IS NOT NULL`, userID).Scan(&n)
	return n
}

func Delete(ctx context.Context, db *sql.DB, userID, id int64) error {
	_, err := db.ExecContext(ctx, `DELETE FROM tasks WHERE id = $1 AND user_id = $2`, id, userID)
	return err
}

// DeleteMany permanently deletes the user's tasks among ids (sub-tasks go with them via ON DELETE CASCADE).
// IDs belonging to other users are ignored. It returns how many top-level rows were removed.
func DeleteMany(ctx context.Context, db *sql.DB, userID int64, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res, err := db.ExecContext(ctx, `DELETE FROM tasks WHERE user_id = $1 AND id = ANY($2)`, userID, pq.Array(ids))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ArchiveMany archives the user's top-level tasks among ids. Statuses are left untouched.
func ArchiveMany(ctx context.Context, db *sql.DB, userID int64, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res, err := db.ExecContext(ctx, `
		UPDATE tasks SET archived_at = now(), updated_at = now()
		WHERE user_id = $1 AND parent_id IS NULL AND archived_at IS NULL AND id = ANY($2)`,
		userID, pq.Array(ids))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
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

// NormPriority coerces arbitrary input (LLM or form) to a known priority, default medium.
func NormPriority(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "low", "medium", "high", "urgent":
		return v
	}
	return "medium"
}

// NormComplexity coerces arbitrary input to a known complexity, default simple.
func NormComplexity(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "simple", "medium", "complex":
		return v
	}
	return "simple"
}
