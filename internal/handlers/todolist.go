package handlers

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"todo/internal/config"
	authmw "todo/internal/middleware"
	"todo/internal/prefs"
	"todo/internal/project"
	"todo/internal/push"
	"todo/internal/task"
	"todo/internal/user"
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
var sizeChoices = []prefChoice{
	{"small", "Kecil"},
	{"medium", "Normal"},
	{"large", "Besar"},
	{"xlarge", "Ekstra"},
}

type remindChoice struct {
	Minutes int
	Label   string
}

var remindChoices = []remindChoice{{0, "Mati"}, {15, "15 menit"}, {30, "30 menit"}, {60, "1 jam"}, {180, "3 jam"}, {1440, "1 hari"}}

var fontChoices = []fontChoice{
	{"system", "Default sistem", "-apple-system, BlinkMacSystemFont, sans-serif"},
	{"inter", "Inter", "'Inter', sans-serif"},
	{"jakarta", "Plus Jakarta Sans", "'Plus Jakarta Sans', sans-serif"},
	{"geist", "Geist", "'Geist', sans-serif"},
}

var priorityChips = []prefChoice{{"urgent", "Urgent"}, {"high", "Tinggi"}, {"medium", "Sedang"}, {"low", "Rendah"}}

// todoURL builds /todo with only the filters that are set, so chips keep each other's state.
func todoURL(project int64, priority, q string) string {
	v := url.Values{}
	if project != 0 {
		v.Set("project", strconv.FormatInt(project, 10))
	}
	if priority != "" {
		v.Set("priority", priority)
	}
	if q != "" {
		v.Set("q", q)
	}
	if len(v) == 0 {
		return "/todo"
	}
	return "/todo?" + v.Encode()
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
		serverError(w, err)
		return
	}
	// ?q= pencarian teks, ?priority= filter prioritas; keduanya digabung dengan ?project=.
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if utf8.RuneCountInString(q) > 100 {
		q = string([]rune(q)[:100])
	}
	prio := r.URL.Query().Get("priority")
	switch prio {
	case "urgent", "high", "medium", "low":
	default:
		prio = ""
	}
	doneLimit := 15
	if q != "" {
		doneLimit = 200 // pencarian juga menjangkau task selesai yang lebih lama
	}
	done, _ := task.ListRecentDone(r.Context(), a.DB, uid, doneLimit)

	// parse filter: ?project=ID (0 = semua)
	var filterID int64
	if raw := r.URL.Query().Get("project"); raw != "" {
		filterID, _ = strconv.ParseInt(raw, 10, 64)
	}
	needle := strings.ToLower(q)
	filter := func(t task.Task) bool {
		if filterID != 0 && (t.ProjectID == nil || *t.ProjectID != filterID) {
			return false
		}
		if prio != "" && t.Priority != prio {
			return false
		}
		if needle != "" && !strings.Contains(strings.ToLower(t.Title+"\n"+t.Description), needle) {
			return false
		}
		return true
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
		"Q":          q,
		"Priority":   prio,
		"Priorities": priorityChips,
		"Here":       todoURL(filterID, prio, q),
		"Filtering":  q != "" || prio != "",
	})
}

func (a *App) SettingsPage(w http.ResponseWriter, r *http.Request) {
	a.renderSettings(w, r, http.StatusOK, "", "")
}

// renderSettings renders the settings page, optionally with an error/info banner.
func (a *App) renderSettings(w http.ResponseWriter, r *http.Request, status int, errMsg, info string) {
	uid := authmw.UserID(r)
	projects, err := project.List(r.Context(), a.DB, uid)
	if err != nil {
		serverError(w, err)
		return
	}
	p, _ := prefs.Get(r.Context(), a.DB, uid)
	me, _ := user.Get(r.Context(), a.DB, uid)
	data := map[string]any{
		"Nav":           "settings",
		"Projects":      projects,
		"DefaultName":   project.DefaultName,
		"Colors":        projectColorPalette,
		"CurrentTheme":  p.Theme,
		"CurrentFont":   p.Font,
		"ThemeChoices":  themeChoices,
		"FontChoices":   fontChoices,
		"CurrentSize":   p.Size,
		"PushKey":       a.Push.PublicKey(),
		"PushDevices":   a.Push.CountSubscriptions(r.Context(), uid),
		"RemindLead":    push.GetLead(r.Context(), a.DB, uid),
		"RemindChoices": remindChoices,
		"SizeChoices":   sizeChoices,
		"Error":         errMsg,
		"Info":          info,
		"ArchivedCount": task.CountArchived(r.Context(), a.DB, uid),
		"Version":       config.Version,
		"Year":          time.Now().Year(),
		"SupportEmail":  a.SupportEmail,
	}
	if me != nil {
		data["Username"] = me.Username
		data["Email"] = me.Email
		data["IsAdmin"] = me.IsAdmin
		if me.IsAdmin {
			data["PendingCount"] = user.PendingCount(r.Context(), a.DB)
		}
	}
	data["_status"] = status
	a.render(w, r, "settings.html", data)
}
