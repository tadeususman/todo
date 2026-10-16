package prefs

import (
	"context"
	"database/sql"
)

type Prefs struct {
	Theme string // system | light | dark
	Font  string // system | inter | jakarta | geist
	Size  string // small | medium | large | xlarge
}

var validThemes = map[string]bool{"system": true, "light": true, "dark": true}
var validFonts = map[string]bool{"system": true, "inter": true, "jakarta": true, "geist": true}

var validSizes = map[string]bool{"small": true, "medium": true, "large": true, "xlarge": true}

func Default() Prefs { return Prefs{Theme: "system", Font: "system", Size: "medium"} }

// EnsureRow inserts a default prefs row for the user if missing. Idempotent.
func EnsureRow(ctx context.Context, db *sql.DB, userID int64) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO user_prefs (user_id) VALUES ($1)
		ON CONFLICT (user_id) DO NOTHING`,
		userID)
	return err
}

func Get(ctx context.Context, db *sql.DB, userID int64) (Prefs, error) {
	p := Default()
	err := db.QueryRowContext(ctx, `SELECT theme, ui_font, ui_font_size FROM user_prefs WHERE user_id = $1`, userID).
		Scan(&p.Theme, &p.Font, &p.Size)
	if err == sql.ErrNoRows {
		return Default(), nil
	}
	return p, err
}

func SetTheme(ctx context.Context, db *sql.DB, userID int64, theme string) error {
	if !validThemes[theme] {
		theme = "system"
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO user_prefs (user_id, theme) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET theme = EXCLUDED.theme, updated_at = now()`,
		userID, theme)
	return err
}

func SetFont(ctx context.Context, db *sql.DB, userID int64, font string) error {
	if !validFonts[font] {
		font = "system"
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO user_prefs (user_id, ui_font) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET ui_font = EXCLUDED.ui_font, updated_at = now()`,
		userID, font)
	return err
}

func SetSize(ctx context.Context, db *sql.DB, userID int64, size string) error {
	if !validSizes[size] {
		size = "medium"
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO user_prefs (user_id, ui_font_size) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET ui_font_size = EXCLUDED.ui_font_size, updated_at = now()`,
		userID, size)
	return err
}
