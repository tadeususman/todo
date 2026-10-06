package handlers

import (
	"bytes"
	"database/sql"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

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

var wibLoc = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		return time.FixedZone("WIB", 7*3600)
	}
	return loc
}()

// toWIB converts a time (value or pointer) to Asia/Jakarta for display.
func toWIB(v any) time.Time {
	switch t := v.(type) {
	case time.Time:
		return t.In(wibLoc)
	case *time.Time:
		if t != nil {
			return t.In(wibLoc)
		}
	}
	return time.Time{}
}

var (
	idDays   = []string{"Minggu", "Senin", "Selasa", "Rabu", "Kamis", "Jumat", "Sabtu"}
	idMonths = []string{"Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agu", "Sep", "Okt", "Nov", "Des"}
)

func dayStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// whenLabel formats a deadline for lists: "15:04" today, "Besok · 15:04", "Kemarin · 15:04", else "Kam, 25 Okt · 15:04".
func whenLabel(v any) string {
	t := toWIB(v)
	if t.IsZero() {
		return ""
	}
	now := time.Now().In(wibLoc)
	clock := t.Format("15:04")
	switch int(dayStart(t).Sub(dayStart(now)).Hours() / 24) {
	case 0:
		return clock
	case 1:
		return "Besok · " + clock
	case -1:
		return "Kemarin · " + clock
	}
	label := idDays[t.Weekday()][:3] + ", " + strconv.Itoa(t.Day()) + " " + idMonths[t.Month()-1]
	if t.Year() != now.Year() {
		label += " " + strconv.Itoa(t.Year())
	}
	return label + " · " + clock
}

// isLate reports whether an unfinished task is past its deadline.
func isLate(v any, status string) bool {
	t := toWIB(v)
	return !t.IsZero() && status != "done" && t.Before(time.Now())
}

type App struct {
	DB         *sql.DB
	Sessions   *session.Manager
	LLM        *llm.Client
	Limiter    *authmw.LoginLimiter
	RegLimiter *authmw.LoginLimiter
	Pages      map[string]*template.Template // one fully-parsed set per page, built at startup
}

// LoadTemplates parses the shared layout/partials once, then clones that base per page
// so each page's own "content" block doesn't clobber the others. Result is read-only → safe for concurrent use.
func LoadTemplates(dir string) map[string]*template.Template {
	files, err := filepath.Glob(filepath.Join(dir, "*.html"))
	if err != nil {
		log.Fatalf("glob templates: %v", err)
	}
	base, err := template.New("").Funcs(template.FuncMap{
		"markdown": renderMarkdown,
		"wib":      toWIB,
		"when":     whenLabel,
		"late":     isLate,
	}).ParseFiles(filepath.Join(dir, "layout.html"))
	if err != nil {
		log.Fatalf("parse layout: %v", err)
	}
	pages := make(map[string]*template.Template, len(files))
	for _, f := range files {
		name := filepath.Base(f)
		if name == "layout.html" {
			continue
		}
		t, err := base.Clone()
		if err != nil {
			log.Fatalf("clone templates: %v", err)
		}
		if _, err := t.ParseFiles(f); err != nil {
			log.Fatalf("parse %s: %v", name, err)
		}
		pages[name] = t
	}
	return pages
}

// render executes the given page template using the shared layout.
// If data is a map[string]any, user prefs (Theme, Font) are auto-injected.
func (a *App) render(w http.ResponseWriter, r *http.Request, name string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	t, ok := a.Pages[name]
	if !ok {
		serverError(w, fmt.Errorf("unknown template %s", name))
		return
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
	if msg := popFlash(w, r); msg != "" {
		data["Flash"] = msg
	}

	// buffer so a template error doesn't leave a half-written page
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", data); err != nil {
		serverError(w, fmt.Errorf("render %s: %w", name, err))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if st, ok := data["_status"].(int); ok && st != http.StatusOK {
		w.WriteHeader(st)
	}
	_, _ = buf.WriteTo(w)
}
