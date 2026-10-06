package handlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"todo/internal/llm"
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

	if utf8.RuneCountInString(input) > maxTaskInput {
		setFlash(w, "Teksnya terlalu panjang (maks. 500 karakter). Ringkas dulu atau pecah jadi beberapa task.")
		http.Redirect(w, r, safeRedirect(r.FormValue("redirect"), "/home"), http.StatusSeeOther)
		return
	}

	// Parse LLM bisa lama; kalau client pindah halaman/submit ulang, task tetap harus tersimpan.
	ctx := context.WithoutCancel(r.Context())

	projects, _ := project.List(ctx, a.DB, uid)
	names := make([]string, 0, len(projects))
	for _, p := range projects {
		names = append(names, p.Name)
	}

	parsed, err := a.LLM.ParseTask(ctx, input, names)
	var notTask *llm.NotTaskError
	if errors.As(err, &notTask) {
		setFlash(w, notTask.Message)
		http.Redirect(w, r, safeRedirect(r.FormValue("redirect"), "/home"), http.StatusSeeOther)
		return
	}
	if err != nil {
		// Fallback: simpan apa adanya sebagai title, biar user ga kehilangan input.
		parsed = &task.Parsed{Title: input, Priority: "medium", Complexity: "simple"}
	}

	projectID := resolveProjectID(projects, parsed.Project)
	if projectID == 0 {
		// no match → default
		if id, err := project.DefaultID(ctx, a.DB, uid); err == nil {
			projectID = id
		}
	}

	if _, err := task.Create(ctx, a.DB, uid, parsed, projectID); err != nil {
		serverError(w, err)
		return
	}
	http.Redirect(w, r, safeRedirect(r.FormValue("redirect"), "/home"), http.StatusSeeOther)
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
	ctx := context.WithoutCancel(r.Context()) // parse LLM bisa lama
	parent, err := task.Get(ctx, a.DB, uid, parentID)
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

	projects, _ := project.List(ctx, a.DB, uid)
	names := make([]string, 0, len(projects))
	for _, p := range projects {
		names = append(names, p.Name)
	}
	if utf8.RuneCountInString(input) > maxTaskInput {
		setFlash(w, "Teksnya terlalu panjang (maks. 500 karakter).")
		http.Redirect(w, r, "/tasks/"+chi.URLParam(r, "id"), http.StatusSeeOther)
		return
	}
	parsed, err := a.LLM.ParseTask(ctx, input, names)
	var notTask *llm.NotTaskError
	if errors.As(err, &notTask) {
		setFlash(w, notTask.Message)
		http.Redirect(w, r, "/tasks/"+chi.URLParam(r, "id"), http.StatusSeeOther)
		return
	}
	if err != nil {
		parsed = &task.Parsed{Title: input, Priority: "medium", Complexity: "simple"}
	}

	var projectID int64
	if parent.ProjectID != nil {
		projectID = *parent.ProjectID
	} else if id, err := project.DefaultID(ctx, a.DB, uid); err == nil {
		projectID = id
	}

	if _, err := task.CreateSub(ctx, a.DB, uid, parentID, parsed, projectID); err != nil {
		serverError(w, err)
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
		"Archived":          task.IsArchived(r.Context(), a.DB, uid, id),
		"Nav":               "home",
		"Task":              t,
		"Subtasks":          subs,
		"SubtasksDone":      doneCount,
		"SubtaskPercent":    subtaskPercent(doneCount, len(subs)),
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
		t, err := time.ParseInLocation("2006-01-02T15:04", dl, loc)
		if err != nil {
			http.Error(w, "Format deadline tidak valid.", http.StatusBadRequest)
			return
		}
		f.Deadline = &t
	}

	// estimated_minutes: tidak lagi diedit lewat UI detail; pertahankan nilai yang ada kecuali form mengirimnya.
	if em, ok := r.Form["estimated_minutes"]; ok {
		if n, err := strconv.Atoi(strings.TrimSpace(em[0])); err == nil && n > 0 {
			f.EstimatedMinutes = &n
		}
	} else if cur, err := task.Get(r.Context(), a.DB, uid, id); err == nil {
		f.EstimatedMinutes = cur.EstimatedMinutes
	}

	if err := task.Update(r.Context(), a.DB, uid, id, f); err != nil {
		serverError(w, err)
		return
	}

	// project_id opsional: ikut disimpan bersama edit lainnya.
	if pv := strings.TrimSpace(r.FormValue("project_id")); pv != "" {
		projectID, err := strconv.ParseInt(pv, 10, 64)
		if err != nil {
			http.Error(w, "bad project id", http.StatusBadRequest)
			return
		}
		if err := task.SetProject(r.Context(), a.DB, uid, id, projectID); err != nil {
			if errors.Is(err, task.ErrProjectInvalid) {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			serverError(w, err)
			return
		}
	}
	if r.Header.Get("X-Requested-With") == "fetch" {
		w.WriteHeader(http.StatusNoContent)
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
		if errors.Is(err, task.ErrInvalidStatus) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		serverError(w, err)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, safeRedirect(r.FormValue("redirect"), backTo(r, "/home")), http.StatusSeeOther)
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
		if errors.Is(err, task.ErrProjectInvalid) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		serverError(w, err)
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
		serverError(w, err)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, safeRedirect(r.FormValue("redirect"), "/home"), http.StatusSeeOther)
}

func subtaskPercent(done, total int) int {
	if total == 0 {
		return 0
	}
	return done * 100 / total
}

func (a *App) taskSetArchived(archived bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := authmw.UserID(r)
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if err := task.SetArchived(r.Context(), a.DB, uid, id, archived); err != nil {
			serverError(w, err)
			return
		}
		if archived {
			setFlash(w, "Task diarsipkan. Lihat di Setting → Arsip.")
			http.Redirect(w, r, "/home", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, safeRedirect(r.FormValue("redirect"), "/tasks/"+chi.URLParam(r, "id")), http.StatusSeeOther)
	}
}

func (a *App) TaskArchive() http.HandlerFunc   { return a.taskSetArchived(true) }
func (a *App) TaskUnarchive() http.HandlerFunc { return a.taskSetArchived(false) }

func (a *App) ArchivePage(w http.ResponseWriter, r *http.Request) {
	tasks, err := task.ListArchived(r.Context(), a.DB, authmw.UserID(r))
	if err != nil {
		serverError(w, err)
		return
	}
	a.render(w, r, "archive.html", map[string]any{"Nav": "settings", "Tasks": tasks})
}
