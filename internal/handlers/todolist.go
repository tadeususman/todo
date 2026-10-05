package handlers

import (
	"net/http"
	"strconv"
	"time"

	authmw "todo/internal/middleware"
	"todo/internal/prefs"
	"todo/internal/project"
	"todo/internal/task"
)

var projectColorPalette = []string{
	"#10b981", // emerald
	"#3b82f6", // blue
	"#8b5cf6", // violet
	"#ec4899", // pink
	"#f59e0b", // amber
	"#ef4444", // red
	"#14b8a6", // teal
	"#64748b", // slate
}

type prefChoice struct {
	Key   string
	Label string
}
type fontChoice struct {
	Key    string
	Label  string
	Sample string
}

var themeChoices = []prefChoice{
	{"system", "Ikut sistem"},
	{"light", "Terang"},
	{"dark", "Gelap"},
}
var fontChoices = []fontChoice{
	{"system", "Default sistem", "-apple-system, BlinkMacSystemFont, sans-serif"},
	{"inter", "Inter", "'Inter', sans-serif"},
	{"jakarta", "Plus Jakarta Sans", "'Plus Jakarta Sans', sans-serif"},
	{"geist", "Geist", "'Geist', sans-serif"},
}

type todoSection struct {
	Key   string
	Label string
	Tasks []task.Task
}

func (a *App) TodoListPage(w http.ResponseWriter, r *http.Request) {
	uid := authmw.UserID(r)

	projects, _ := project.List(r.Context(), a.DB, uid)
	open, err := task.ListOpen(r.Context(), a.DB, uid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	done, _ := task.ListRecentDone(r.Context(), a.DB, uid, 15)

	// parse filter: ?project=ID (0 = semua)
	var filterID int64
	if raw := r.URL.Query().Get("project"); raw != "" {
		filterID, _ = strconv.ParseInt(raw, 10, 64)
	}
	filter := func(t task.Task) bool {
		if filterID == 0 {
			return true
		}
		return t.ProjectID != nil && *t.ProjectID == filterID
	}

	loc, _ := time.LoadLocation("Asia/Jakarta")
	now := time.Now().In(loc)
	startToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	endToday := startToday.Add(24 * time.Hour)
	endTomorrow := startToday.Add(48 * time.Hour)
	endWeek := startToday.Add(7 * 24 * time.Hour)

	buckets := map[string]*todoSection{
		"overdue":  {Key: "overdue", Label: "Lewat deadline"},
		"today":    {Key: "today", Label: "Hari ini"},
		"tomorrow": {Key: "tomorrow", Label: "Besok"},
		"week":     {Key: "week", Label: "Minggu ini"},
		"later":    {Key: "later", Label: "Nanti"},
		"nodate":   {Key: "nodate", Label: "Tanpa tanggal"},
		"done":     {Key: "done", Label: "Selesai"},
	}

	for _, t := range open {
		if !filter(t) {
			continue
		}
		switch {
		case t.Deadline == nil:
			buckets["nodate"].Tasks = append(buckets["nodate"].Tasks, t)
		case t.Deadline.Before(startToday):
			buckets["overdue"].Tasks = append(buckets["overdue"].Tasks, t)
		case t.Deadline.Before(endToday):
			buckets["today"].Tasks = append(buckets["today"].Tasks, t)
		case t.Deadline.Before(endTomorrow):
			buckets["tomorrow"].Tasks = append(buckets["tomorrow"].Tasks, t)
		case t.Deadline.Before(endWeek):
			buckets["week"].Tasks = append(buckets["week"].Tasks, t)
		default:
			buckets["later"].Tasks = append(buckets["later"].Tasks, t)
		}
	}
	for _, t := range done {
		if !filter(t) {
			continue
		}
		buckets["done"].Tasks = append(buckets["done"].Tasks, t)
	}

	// ordered sections
	order := []string{"overdue", "today", "tomorrow", "week", "later", "nodate", "done"}
	sections := make([]todoSection, 0, len(order))
	totalOpen := 0
	for _, k := range order {
		s := buckets[k]
		if len(s.Tasks) == 0 {
			continue
		}
		sections = append(sections, *s)
		if k != "done" {
			totalOpen += len(s.Tasks)
		}
	}

	a.render(w, r, "todo.html", map[string]any{
		"Nav":        "todo",
		"Projects":   projects,
		"Sections":   sections,
		"FilterID":   filterID,
		"TotalOpen":  totalOpen,
		"ProjectAll": "Semua",
	})
}

func (a *App) SettingsPage(w http.ResponseWriter, r *http.Request) {
	uid := authmw.UserID(r)
	projects, err := project.List(r.Context(), a.DB, uid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	p, _ := prefs.Get(r.Context(), a.DB, uid)
	a.render(w, r, "settings.html", map[string]any{
		"Nav":           "settings",
		"Projects":      projects,
		"DefaultName":   project.DefaultName,
		"Colors":        projectColorPalette,
		"CurrentTheme":  p.Theme,
		"CurrentFont":   p.Font,
		"ThemeChoices":  themeChoices,
		"FontChoices":   fontChoices,
	})
}
