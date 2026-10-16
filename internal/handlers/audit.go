package handlers

import (
	"log"
	"net/http"

	authmw "todo/internal/middleware"
)

// audit records a security-relevant event (who, what, on whom, from where). Never put secrets or free text in target.
// actor 0 means "the logged-in user of this request" (or nobody, for unauthenticated events).
func (a *App) audit(r *http.Request, actor int64, action, target string) {
	if actor == 0 {
		actor = authmw.UserID(r)
	}
	var actorArg any
	if actor != 0 {
		actorArg = actor
	}
	_, err := a.DB.ExecContext(r.Context(),
		`INSERT INTO audit_log (actor_id, action, target, ip) VALUES ($1, $2, $3, $4)`,
		actorArg, action, target, authmw.ClientIP(r))
	if err != nil {
		log.Printf("audit %s: %v", action, err)
	}
}
