package handlers

import (
	"net/http"
	"time"

	authmw "todo/internal/middleware"
	"todo/internal/task"
)

func (a *App) HomePage(w http.ResponseWriter, r *http.Request) {
	uid := authmw.UserID(r)

	var username string
	_ = a.DB.QueryRowContext(r.Context(), `SELECT username FROM users WHERE id = $1`, uid).Scan(&username)

	all, err := task.ListOpen(r.Context(), a.DB, uid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	loc, _ := time.LoadLocation("Asia/Jakarta")
	now := time.Now().In(loc)
	startToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	endToday := startToday.Add(24 * time.Hour)

	var overdue, today []task.Task
	for _, t := range all {
		if t.Deadline == nil {
			continue
		}
		switch {
		case t.Deadline.Before(startToday):
			overdue = append(overdue, t)
		case t.Deadline.Before(endToday):
			today = append(today, t)
		}
	}
	a.render(w, r, "home.html", map[string]any{
		"Nav":       "home",
		"Username":  username,
		"TodayDate": now.Format("Monday, 2 Jan 2006"),
		"Overdue":   overdue,
		"Today":     today,
	})
}
