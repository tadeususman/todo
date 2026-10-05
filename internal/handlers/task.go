package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	authmw "todo/internal/middleware"
	"todo/internal/project"
	"todo/internal/task"
)

// TaskCreate — accepts natural-language input, routes through LLM parse, inserts.
func (a *App) TaskCreate(w http.ResponseWriter, r *http.Request) {
	uid := authmw.UserID(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	input := strings.TrimSpace(r.FormValue("input"))
	if input == "" {
		http.Redirect(w, r, "/home", http.StatusSeeOther)
		return
	}

	projects, _ := project.List(r.Context(), a.DB, uid)
	names := make([]string, 0, len(projects))
	for _, p := range projects {
		names = append(names, p.Name)
	}

	parsed, err := a.LLM.ParseTask(r.Context(), input, names)
	if err != nil {
		// Fallback: simpan apa adanya sebagai title, biar user ga kehilangan input.
		parsed = &task.Parsed{Title: input, Priority: "medium", Complexity: "simple"}
	}

	projectID := resolveProjectID(projects, parsed.Project)
	if projectID == 0 {
		// no match → default
		if id, err := project.DefaultID(r.Context(), a.DB, uid); err == nil {
			projectID = id
		}
	}

	if _, err := task.Create(r.Context(), a.DB, uid, parsed, projectID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	redirect := r.FormValue("redirect")
	if redirect == "" || !strings.HasPrefix(redirect, "/") {
		redirect = "/home"
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

func resolveProjectID(projects []project.Project, name string) int64 {
	if name == "" {
		return 0
	}
	target := strings.ToLower(strings.TrimSpace(name))
	for _, p := range projects {
		if strings.ToLower(p.Name) == target {
			return p.ID
		}
	}
	return 0
}

// TaskCreateSubtask — adds a subtask under the given parent, inheriting the parent's project.
func (a *App) TaskCreateSubtask(w http.ResponseWriter, r *http.Request) {
	uid := authmw.UserID(r)
	parentID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	parent, err := task.Get(r.Context(), a.DB, uid, parentID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	input := strings.TrimSpace(r.FormValue("input"))
	if input == "" {
		http.Redirect(w, r, "/tasks/"+chi.URLParam(r, "id"), http.StatusSeeOther)
		return
	}

	projects, _ := project.List(r.Context(), a.DB, uid)
	names := make([]string, 0, len(projects))
	for _, p := range projects {
		names = append(names, p.Name)
	}
	parsed, err := a.LLM.ParseTask(r.Context(), input, names)
	if err != nil {
		parsed = &task.Parsed{Title: input, Priority: "medium", Complexity: "simple"}
	}

	var projectID int64
	if parent.ProjectID != nil {
		projectID = *parent.ProjectID
	} else if id, err := project.DefaultID(r.Context(), a.DB, uid); err == nil {
		projectID = id
	}

	if _, err := task.CreateSub(r.Context(), a.DB, uid, parentID, parsed, projectID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/tasks/"+chi.URLParam(r, "id"), http.StatusSeeOther)
}

func (a *App) TaskView(w http.ResponseWriter, r *http.Request) {
	uid := authmw.UserID(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	t, err := task.Get(r.Context(), a.DB, uid, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	subs, _ := task.ListSubtasks(r.Context(), a.DB, uid, id)
	doneCount := 0
	for _, s := range subs {
		if s.Status == "done" {
			doneCount++
		}
	}
	projects, _ := project.List(r.Context(), a.DB, uid)
	var selected int64
	if t.ProjectID != nil {
		selected = *t.ProjectID
	}
	// deadline formatted for <input type="datetime-local"> in Jakarta tz
	var deadlineInput string
	if t.Deadline != nil {
		loc, _ := time.LoadLocation("Asia/Jakarta")
		deadlineInput = t.Deadline.In(loc).Format("2006-01-02T15:04")
	}
	a.render(w, r, "task_detail.html", map[string]any{
		"Nav":               "home",
		"Task":              t,
		"Subtasks":          subs,
		"SubtasksDone":      doneCount,
		"Projects":          projects,
		"SelectedProjectID": selected,
		"DeadlineInput":     deadlineInput,
		"Priorities":        []string{"low", "medium", "high", "urgent"},
		"Complexities":      []string{"simple", "medium", "complex"},
	})
}

func (a *App) TaskEdit(w http.ResponseWriter, r *http.Request) {
	uid := authmw.UserID(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		http.Error(w, "title kosong", http.StatusBadRequest)
		return
	}
	f := task.UpdateFields{
		Title:       title,
		Description: strings.TrimSpace(r.FormValue("description")),
		Priority:    r.FormValue("priority"),
		Complexity:  r.FormValue("complexity"),
	}

	// deadline: datetime-local in Jakarta tz → *time.Time
	if dl := strings.TrimSpace(r.FormValue("deadline")); dl != "" {
		loc, _ := time.LoadLocation("Asia/Jakarta")
		if t, err := time.ParseInLocation("2006-01-02T15:04", dl, loc); err == nil {
			f.Deadline = &t
		}
	}

	// estimated_minutes: optional int
	if em := strings.TrimSpace(r.FormValue("estimated_minutes")); em != "" {
		if n, err := strconv.Atoi(em); err == nil && n > 0 {
			f.EstimatedMinutes = &n
		}
	}

	if err := task.Update(r.Context(), a.DB, uid, id, f); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/tasks/"+chi.URLParam(r, "id"), http.StatusSeeOther)
}

func (a *App) TaskUpdateStatus(w http.ResponseWriter, r *http.Request) {
	uid := authmw.UserID(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	status := r.FormValue("status")
	if err := task.UpdateStatus(r.Context(), a.DB, uid, id, status); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, "/home", http.StatusSeeOther)
}

func (a *App) TaskSetProject(w http.ResponseWriter, r *http.Request) {
	uid := authmw.UserID(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	projectID, err := strconv.ParseInt(r.FormValue("project_id"), 10, 64)
	if err != nil {
		http.Error(w, "bad project id", http.StatusBadRequest)
		return
	}
	if err := task.SetProject(r.Context(), a.DB, uid, id, projectID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/tasks/"+chi.URLParam(r, "id"), http.StatusSeeOther)
}

func (a *App) TaskDelete(w http.ResponseWriter, r *http.Request) {
	uid := authmw.UserID(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := task.Delete(r.Context(), a.DB, uid, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, "/home", http.StatusSeeOther)
}
