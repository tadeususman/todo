package handlers

import (
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"todo/internal/chat"
	authmw "todo/internal/middleware"
	"todo/internal/project"
	"todo/internal/task"
)

func (a *App) ChatPage(w http.ResponseWriter, r *http.Request) {
	uid := authmw.UserID(r)
	var username string
	_ = a.DB.QueryRowContext(r.Context(), `SELECT username FROM users WHERE id = $1`, uid).Scan(&username)
	msgs, _ := chat.List(r.Context(), a.DB, uid, 200)
	a.render(w, r, "chat.html", map[string]any{
		"Nav":      "chat",
		"Username": username,
		"Messages": msgs,
	})
}

func (a *App) ChatSend(w http.ResponseWriter, r *http.Request) {
	uid := authmw.UserID(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	input := strings.TrimSpace(r.FormValue("input"))
	if input == "" {
		http.Redirect(w, r, "/chat", http.StatusSeeOther)
		return
	}

	var username string
	_ = a.DB.QueryRowContext(r.Context(), `SELECT username FROM users WHERE id = $1`, uid).Scan(&username)

	userID, err := chat.Save(r.Context(), a.DB, uid, chat.RoleUser, input)
	if err != nil {
		serverError(w, err)
		return
	}

	history, _ := chat.List(r.Context(), a.DB, uid, 50)
	// drop the just-saved user message so Chat() doesn't see it twice
	if n := len(history); n > 0 && history[n-1].Role == chat.RoleUser {
		history = history[:n-1]
	}
	tasks, _ := task.ListOpen(r.Context(), a.DB, uid)
	projects, _ := project.List(r.Context(), a.DB, uid)

	reply, err := a.LLM.Chat(r.Context(), username, history, input, tasks, projects)
	if err != nil {
		log.Printf("chat llm: %v", err)
		reply = "Maaf, asisten lagi tidak bisa dihubungi. Coba lagi sebentar."
	}
	assistantID, _ := chat.Save(r.Context(), a.DB, uid, chat.RoleAssistant, reply)

	// JS fetch mode → return HTML snippet of both bubbles so client can replace optimistic + typing
	if r.Header.Get("X-Chat-Fetch") == "1" {
		um, _ := chat.Get(r.Context(), a.DB, uid, userID)
		am, _ := chat.Get(r.Context(), a.DB, uid, assistantID)
		data := map[string]any{"User": um, "Assistant": am}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := a.Pages["chat.html"].ExecuteTemplate(w, "chat-pair", data); err != nil {
			serverError(w, err)
		}
		return
	}

	http.Redirect(w, r, "/chat", http.StatusSeeOther)
}

func (a *App) ChatDelete(w http.ResponseWriter, r *http.Request) {
	uid := authmw.UserID(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := chat.Delete(r.Context(), a.DB, uid, id); err != nil {
		serverError(w, err)
		return
	}
	http.Redirect(w, r, "/chat", http.StatusSeeOther)
}

func (a *App) ChatClear(w http.ResponseWriter, r *http.Request) {
	uid := authmw.UserID(r)
	if err := chat.Clear(r.Context(), a.DB, uid); err != nil {
		serverError(w, err)
		return
	}
	http.Redirect(w, r, "/chat", http.StatusSeeOther)
}
