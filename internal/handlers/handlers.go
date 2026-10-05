package handlers

import (
	"database/sql"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"regexp"

	"todo/internal/llm"
	authmw "todo/internal/middleware"
	"todo/internal/prefs"
	"todo/internal/session"
)

var (
	mdBold   = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)
	mdItalic = regexp.MustCompile(`\*([^*\n]+)\*`)
	mdCode   = regexp.MustCompile("`([^`\n]+)`")
)

// renderMarkdown applies minimal markdown transforms safely: HTML-escape first,
// then convert **bold**, *italic*, and `inline code`. Line breaks are handled
// by CSS `white-space: pre-wrap` on the bubble.
func renderMarkdown(s string) template.HTML {
	esc := template.HTMLEscapeString(s)
	esc = mdBold.ReplaceAllString(esc, `<strong>$1</strong>`)
	esc = mdItalic.ReplaceAllString(esc, `<em>$1</em>`)
	esc = mdCode.ReplaceAllString(esc, `<code>$1</code>`)
	return template.HTML(esc)
}

type App struct {
	DB       *sql.DB
	Sessions *session.Manager
	LLM      *llm.Client
	Tmpl     *template.Template
}

// LoadTemplates parses all .html under dir with custom funcs.
func LoadTemplates(dir string) *template.Template {
	pattern := filepath.Join(dir, "*.html")
	t, err := template.New("").Funcs(template.FuncMap{
		"markdown": renderMarkdown,
	}).ParseGlob(pattern)
	if err != nil {
		log.Fatalf("parse templates: %v", err)
	}
	return t
}

// render executes the given template using the shared layout.
// Each page template defines its own "content" block that layout.html slots in.
// If data is a map[string]any, user prefs (Theme, Font) are auto-injected.
func (a *App) render(w http.ResponseWriter, r *http.Request, name string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	// defaults for unauthenticated pages
	theme, font := "light", "system"
	if uid := authmw.UserID(r); uid != 0 {
		if p, err := prefs.Get(r.Context(), a.DB, uid); err == nil {
			theme, font = p.Theme, p.Font
		}
	}
	data["Theme"] = theme
	data["Font"] = font

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Clone so each page can define its own "content" without clobbering others.
	t, err := a.Tmpl.Clone()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := t.ParseFiles(filepath.Join("web/templates", name)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := t.ExecuteTemplate(w, "layout", data); err != nil {
		log.Printf("render %s: %v", name, err)
	}
}
