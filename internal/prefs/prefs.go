package prefs

import (
	"context"
	"database/sql"
)

type Prefs struct {
	Theme string // system | light | dark
	Font  string // system | inter | jakarta | geist
}

var validThemes = map[string]bool{"system": true, "light": true, "dark": true}
var validFonts = map[string]bool{"system": true, "inter": true, "jakarta": true, "geist": true}

func Default() Prefs { return Prefs{Theme: "system", Font: "system"} }

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
	err := db.QueryRowContext(ctx, `SELECT theme, ui_font FROM user_prefs WHERE user_id = $1`, userID).
		Scan(&p.Theme, &p.Font)
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
