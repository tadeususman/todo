// Package push delivers deadline reminders to browsers via Web Push (VAPID).
package push

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

type Service struct {
	DB      *sql.DB
	Subject string // VAPID "sub": https URL or mailto:
	pub     string
	priv    string
	client  *http.Client
}

// New loads the VAPID key pair from the database, creating it on first start so subscriptions survive restarts.
func New(ctx context.Context, db *sql.DB, subject string) (*Service, error) {
	s := &Service{DB: db, Subject: subject, client: &http.Client{Timeout: 15 * time.Second}}
	get := func(k string) string {
		var v string
		_ = db.QueryRowContext(ctx, `SELECT value FROM app_kv WHERE key = $1`, k).Scan(&v)
		return v
	}
	s.pub, s.priv = get("vapid_public"), get("vapid_private")
	if s.pub == "" || s.priv == "" {
		priv, pub, err := webpush.GenerateVAPIDKeys()
		if err != nil {
			return nil, err
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback()
		for k, v := range map[string]string{"vapid_public": pub, "vapid_private": priv} {
			if _, err := tx.ExecContext(ctx, `INSERT INTO app_kv (key, value) VALUES ($1, $2) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, k, v); err != nil {
				return nil, err
			}
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		s.pub, s.priv = pub, priv
		log.Print("[push] generated new VAPID keys")
	}
	return s, nil
}

// PublicKey is the application server key the browser needs to subscribe.
func (s *Service) PublicKey() string { return s.pub }

var ErrBadEndpoint = errors.New("endpoint push tidak valid")

// allowedHosts are the browser vendors' push services. The server POSTs to whatever endpoint a client registers,
// so anything else would let a user make the server call arbitrary URLs (SSRF).
var allowedHosts = []string{"fcm.googleapis.com", "android.googleapis.com", "push.services.mozilla.com", "push.apple.com", "notify.windows.com"}

func validEndpoint(raw string) bool {
	if len(raw) > 2048 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	h := strings.ToLower(u.Hostname())
	for _, a := range allowedHosts {
		if h == a || strings.HasSuffix(h, "."+a) {
			return true
		}
	}
	return false
}

func (s *Service) Subscribe(ctx context.Context, userID int64, endpoint, p256dh, auth string) error {
	if !validEndpoint(endpoint) || p256dh == "" || auth == "" || len(p256dh) > 256 || len(auth) > 64 {
		return ErrBadEndpoint
	}
	// An endpoint belongs to one browser profile; if it was registered by someone else earlier, it moves to this user.
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth) VALUES ($1, $2, $3, $4)
		ON CONFLICT (endpoint) DO UPDATE SET user_id = EXCLUDED.user_id, p256dh = EXCLUDED.p256dh, auth = EXCLUDED.auth`,
		userID, endpoint, p256dh, auth)
	return err
}

func (s *Service) Unsubscribe(ctx context.Context, userID int64, endpoint string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE user_id = $1 AND endpoint = $2`, userID, endpoint)
	return err
}

func (s *Service) CountSubscriptions(ctx context.Context, userID int64) int {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM push_subscriptions WHERE user_id = $1`, userID).Scan(&n)
	return n
}

type Message struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
}

// Send pushes msg to every device of the user and drops subscriptions the push service says are gone.
// It returns how many deliveries succeeded.
func (s *Service) Send(ctx context.Context, userID int64, msg Message) (int, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, endpoint, p256dh, auth FROM push_subscriptions WHERE user_id = $1`, userID)
	if err != nil {
		return 0, err
	}
	type sub struct {
		id                   int64
		endpoint, p256dh, au string
	}
	var subs []sub
	for rows.Next() {
		var x sub
		if err := rows.Scan(&x.id, &x.endpoint, &x.p256dh, &x.au); err != nil {
			rows.Close()
			return 0, err
		}
		subs = append(subs, x)
	}
	rows.Close()

	payload, _ := json.Marshal(msg)
	ok := 0
	for _, x := range subs {
		if !validEndpoint(x.endpoint) { // defense in depth for rows written before the allowlist
			_, _ = s.DB.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE id = $1`, x.id)
			continue
		}
		resp, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
			Endpoint: x.endpoint, Keys: webpush.Keys{P256dh: x.p256dh, Auth: x.au},
		}, &webpush.Options{Subscriber: s.Subject, VAPIDPublicKey: s.pub, VAPIDPrivateKey: s.priv, TTL: 3600, Urgency: webpush.UrgencyHigh, HTTPClient: s.client})
		if err != nil {
			log.Printf("[push] send: %v", err)
			continue
		}
		resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
			_, _ = s.DB.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE id = $1`, x.id)
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			ok++
		default:
			log.Printf("[push] push service answered %d", resp.StatusCode)
		}
	}
	return ok, nil
}

var wib = func() *time.Location {
	if l, err := time.LoadLocation("Asia/Jakarta"); err == nil {
		return l
	}
	return time.FixedZone("WIB", 7*3600)
}()

// RemindDue sends one reminder per task whose deadline falls within the user's lead time.
// Only users with at least one device subscribed and lead time > 0 are considered.
func (s *Service) RemindDue(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT t.id, t.user_id, t.title, t.deadline
		FROM tasks t JOIN user_prefs p ON p.user_id = t.user_id
		WHERE t.reminded_at IS NULL AND t.deadline IS NOT NULL
		  AND t.status IN ('pending', 'in_progress') AND t.archived_at IS NULL
		  AND t.deadline > now() AND t.deadline <= now() + make_interval(mins => p.remind_lead_min)
		  AND p.remind_lead_min > 0
		  AND EXISTS (SELECT 1 FROM push_subscriptions s WHERE s.user_id = t.user_id)
		LIMIT 200`)
	if err != nil {
		return err
	}
	type due struct {
		id, user int64
		title    string
		deadline time.Time
	}
	var list []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.id, &d.user, &d.title, &d.deadline); err != nil {
			rows.Close()
			return err
		}
		list = append(list, d)
	}
	rows.Close()

	for _, d := range list {
		// Claim first so a slow push service or a restart can't cause duplicates.
		res, err := s.DB.ExecContext(ctx, `UPDATE tasks SET reminded_at = now() WHERE id = $1 AND reminded_at IS NULL`, d.id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue
		}
		mins := int(time.Until(d.deadline).Minutes()) + 1
		body := fmt.Sprintf("%s · %s", d.title, d.deadline.In(wib).Format("15:04"))
		title := fmt.Sprintf("Deadline %d menit lagi", mins)
		if mins >= 60 {
			title = fmt.Sprintf("Deadline sekitar %d jam lagi", (mins+30)/60)
		}
		if _, err := s.Send(ctx, d.user, Message{Title: title, Body: body, URL: fmt.Sprintf("/tasks/%d", d.id)}); err != nil {
			log.Printf("[push] remind task %d: %v", d.id, err)
		}
	}
	return nil
}

// Run checks for due reminders every minute until ctx ends.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		if err := s.RemindDue(ctx); err != nil && ctx.Err() == nil {
			log.Printf("[push] remind: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// SetLead stores how many minutes before a deadline the reminder fires (0 disables).
func SetLead(ctx context.Context, db *sql.DB, userID int64, minutes int) error {
	if !ValidLead(minutes) {
		minutes = 60
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO user_prefs (user_id, remind_lead_min) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET remind_lead_min = EXCLUDED.remind_lead_min, updated_at = now()`, userID, minutes)
	return err
}

var Leads = []int{0, 15, 30, 60, 180, 1440}

func ValidLead(m int) bool {
	for _, l := range Leads {
		if l == m {
			return true
		}
	}
	return false
}

func GetLead(ctx context.Context, db *sql.DB, userID int64) int {
	m := 60
	_ = db.QueryRowContext(ctx, `SELECT remind_lead_min FROM user_prefs WHERE user_id = $1`, userID).Scan(&m)
	return m
}
