package handlers

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"todo/internal/chat"
	authmw "todo/internal/middleware"
	"todo/internal/project"
	"todo/internal/task"
)

// chatItem is a message plus, when it opens a new calendar day (WIB), the separator label to show above it.
type chatItem struct {
	Msg chat.Message
	Day string
}

func chatItems(msgs []chat.Message, now time.Time) []chatItem {
	items := make([]chatItem, 0, len(msgs))
	var prev time.Time
	for _, m := range msgs {
		it := chatItem{Msg: m}
		if prev.IsZero() || !sameDay(prev, m.CreatedAt) {
			it.Day = dayLabel(m.CreatedAt, now)
		}
		prev = m.CreatedAt
		items = append(items, it)
	}
	return items
}

func sameDay(a, b time.Time) bool {
	a, b = a.In(wibLoc), b.In(wibLoc)
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

func (a *App) ChatPage(w http.ResponseWriter, r *http.Request) {
	uid := authmw.UserID(r)
	var username string
	_ = a.DB.QueryRowContext(r.Context(), `SELECT username FROM users WHERE id = $1`, uid).Scan(&username)
	msgs, _ := chat.List(r.Context(), a.DB, uid, 200)
	a.render(w, r, "chat.html", map[string]any{
		"Nav":      "chat",
		"Username": username,
		"Messages": msgs,
		"Items":    chatItems(msgs, time.Now()),
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
	// the message before this one decides whether the reply bubble needs a new-day separator
	var prevAt time.Time
	if n := len(history); n > 0 {
		prevAt = history[n-1].CreatedAt
	}
	tasks, _ := task.ListOpen(r.Context(), a.DB, uid)
	projects, _ := project.List(r.Context(), a.DB, uid)

	reply, proposed, err := a.LLM.Chat(r.Context(), username, history, input, tasks, projects)
	if err != nil {
		log.Printf("chat llm: %v", err)
		reply = "Maaf, asisten lagi tidak bisa dihubungi. Coba lagi sebentar."
		proposed = nil
	}
	// LLM output is untrusted: keep only changes to this user's own tasks with acceptable values.
	actions := chat.Prepare(r.Context(), a.DB, uid, proposed)
	assistantID, _ := chat.SaveWithActions(r.Context(), a.DB, uid, chat.RoleAssistant, reply, actions)

	// JS fetch mode → return HTML snippet of both bubbles so client can replace optimistic + typing
	if r.Header.Get("X-Chat-Fetch") == "1" {
		um, _ := chat.Get(r.Context(), a.DB, uid, userID)
		am, _ := chat.Get(r.Context(), a.DB, uid, assistantID)
		data := map[string]any{"User": um, "Assistant": am}
		if um != nil && (prevAt.IsZero() || !sameDay(prevAt, um.CreatedAt)) {
			data["Day"] = dayLabel(um.CreatedAt, time.Now())
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := a.Pages["chat.html"].ExecuteTemplate(w, "chat-pair", data); err != nil {
			serverError(w, err)
		}
		return
	}

	http.Redirect(w, r, "/chat", http.StatusSeeOther)
}

// ChatApply runs the changes the assistant proposed on a message, after the user confirmed them.
func (a *App) ChatApply(w http.ResponseWriter, r *http.Request) {
	uid := authmw.UserID(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	actions, err := chat.ClaimActions(r.Context(), a.DB, uid, id, chat.ActionApplied)
	if errors.Is(err, chat.ErrNoPending) {
		setFlash(w, "Perubahan ini sudah diproses.")
		http.Redirect(w, r, "/chat", http.StatusSeeOther)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	ok, failed := 0, 0
	for _, act := range actions {
		if err := chat.Apply(r.Context(), a.DB, uid, act); err != nil {
			log.Printf("chat apply task=%d field=%s: %v", act.TaskID, act.Field, err)
			failed++
			continue
		}
		ok++
	}
	msg := strconv.Itoa(ok) + " perubahan diterapkan."
	if failed > 0 {
		msg += " " + strconv.Itoa(failed) + " gagal (task mungkin sudah dihapus)."
	}
	setFlash(w, msg)
	http.Redirect(w, r, "/chat", http.StatusSeeOther)
}

// ChatDismiss drops the proposed changes without applying them.
func (a *App) ChatDismiss(w http.ResponseWriter, r *http.Request) {
	uid := authmw.UserID(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := chat.ClaimActions(r.Context(), a.DB, uid, id, chat.ActionDismissed); err != nil && !errors.Is(err, chat.ErrNoPending) {
		serverError(w, err)
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
