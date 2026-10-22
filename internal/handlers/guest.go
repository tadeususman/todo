package handlers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	authmw "todo/internal/middleware"
	"todo/internal/task"
	"todo/internal/user"
)

var errGuestAIFull = errors.New("guest ai quota spent")

const guestAIFull = "Kuota AI mode coba sudah habis. Daftar untuk memakai AI tanpa batas."

// GuestStart creates a temporary guest account and signs the visitor in.
func (a *App) GuestStart(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.Sessions.UserID(r); ok {
		http.Redirect(w, r, "/home", http.StatusSeeOther)
		return
	}
	// Setiap percobaan dihitung supaya satu jaringan tidak bisa membanjiri tabel user.
	if a.GuestLimiter.Blocked(r) {
		a.render(w, r, "login.html", map[string]any{"_status": http.StatusTooManyRequests, "Error": "Terlalu banyak percobaan mode coba dari jaringan ini. Coba lagi nanti."})
		return
	}
	a.GuestLimiter.Fail(r)
	id, err := user.CreateGuest(r.Context(), a.DB)
	if err == user.ErrTooManyGuests {
		a.render(w, r, "login.html", map[string]any{"_status": http.StatusServiceUnavailable, "Error": err.Error()})
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	a.audit(r, id, "guest_start", "")
	if err := a.Sessions.Create(w, r, id); err != nil {
		http.Error(w, "could not start session", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/home", http.StatusSeeOther)
}

// AccountClaim upgrades the guest to a real sign-up; their tasks stay, pending admin approval like any registration.
func (a *App) AccountClaim(w http.ResponseWriter, r *http.Request) {
	uid := authmw.UserID(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if r.FormValue("password") != r.FormValue("confirm") {
		a.renderSettings(w, r, http.StatusBadRequest, "Konfirmasi password tidak sama.", "")
		return
	}
	if a.RegLimiter.Blocked(r) {
		a.renderSettings(w, r, http.StatusTooManyRequests, "Terlalu banyak pendaftaran dari jaringan ini. Coba lagi nanti.", "")
		return
	}
	a.RegLimiter.Fail(r)
	err := user.ClaimGuest(r.Context(), a.DB, uid, r.FormValue("username"), r.FormValue("email"), r.FormValue("password"))
	if err == user.ErrNotGuest {
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}
	if err != nil {
		if isUserFacing(err) {
			a.renderSettings(w, r, http.StatusBadRequest, err.Error(), "")
			return
		}
		serverError(w, err)
		return
	}
	a.audit(r, uid, "guest_claim", "")
	a.notifySignup(r, "tamu mendaftar dari mode coba", r.FormValue("username"), r.FormValue("email"))
	a.Sessions.Destroy(w, r)
	a.render(w, r, "register_done.html", map[string]any{"Claimed": true})
}

// allowAI reports whether this user may spend an AI call now; guests have a small fixed quota.
func (a *App) allowAI(r *http.Request, uid int64) bool {
	ok := user.UseAI(r.Context(), a.DB, uid)
	if !ok {
		log.Printf("ai quota reached user=%d", uid)
	}
	return ok
}

func guestLeftLabel(until time.Time) string {
	d := time.Until(until)
	switch {
	case d <= 0:
		return "habis"
	case d < time.Hour:
		return fmt.Sprintf("%d menit lagi", int(d.Minutes())+1)
	}
	return fmt.Sprintf("%d jam lagi", int(d.Hours()))
}

// parseTask runs the LLM parse unless a guest has used up the quota; then the input is saved as-is
// (same fallback as an LLM failure) and the user is told why.
func (a *App) parseTask(ctx context.Context, r *http.Request, uid int64, input string, projectNames []string) (*task.Parsed, error) {
	if !a.allowAI(r, uid) {
		return nil, errGuestAIFull
	}
	return a.LLM.ParseTask(ctx, input, projectNames)
}

// notifySignup tells the admin on Telegram that an account now waits for approval.
func (a *App) notifySignup(r *http.Request, title, username, email string) {
	if !a.Notify.Enabled() {
		return
	}
	a.Notify.Send(fmt.Sprintf("todo: %s\n%s (%s)\nMenunggu tindakan admin: %d\nBuka Setting → Kelola user untuk memproses.",
		title, strings.TrimSpace(username), strings.ToLower(strings.TrimSpace(email)), user.PendingCount(r.Context(), a.DB)))
}
