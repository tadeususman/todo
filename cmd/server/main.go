package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"
	"golang.org/x/crypto/bcrypt"

	appconfig "todo/internal/config"
	appdb "todo/internal/db"
	"todo/internal/handlers"
	"todo/internal/llm"
	authmw "todo/internal/middleware"
	"todo/internal/prefs"
	"todo/internal/project"
	"todo/internal/session"
)

func main() {
	_ = godotenv.Load() // optional: in docker-compose env is injected
	cfg := appconfig.Load()
	ctx := context.Background()

	pool, err := appdb.Connect(ctx, cfg.DBUrl)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	if err := bootstrapAdmin(ctx, pool, cfg.AdminUsername, cfg.AdminPassword); err != nil {
		log.Fatalf("bootstrap admin: %v", err)
	}

	var adminID int64
	if err := pool.QueryRowContext(ctx, `SELECT id FROM users WHERE is_admin AND status = 'approved' ORDER BY id LIMIT 1`).Scan(&adminID); err != nil {
		log.Fatalf("locate admin: %v", err)
	}
	if err := project.EnsureDefault(ctx, pool, adminID); err != nil {
		log.Fatalf("seed default project: %v", err)
	}
	if err := prefs.EnsureRow(ctx, pool, adminID); err != nil {
		log.Fatalf("seed prefs: %v", err)
	}

	sessions := session.NewManager(pool)
	go func() {
		for {
			if err := sessions.PurgeExpired(ctx); err != nil {
				log.Printf("purge sessions: %v", err)
			}
			time.Sleep(time.Hour)
		}
	}()
	aiClient := llm.New(llm.Config{
		Provider:    cfg.AIProvider,
		BridgeURL:   cfg.BridgeURL,
		BridgeModel: cfg.BridgeModel,
		GeminiKey:   cfg.GeminiKey,
		GeminiModel: cfg.GeminiModel,
	}, pool)
	tmpl := handlers.LoadTemplates("web/templates")

	app := &handlers.App{
		DB:         pool,
		Sessions:   sessions,
		LLM:        aiClient,
		Limiter:    authmw.NewLoginLimiter(5, 15*time.Minute),
		RegLimiter: authmw.NewLoginLimiter(5, time.Hour),
		Pages:      tmpl,
	}

	r := chi.NewRouter()
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(authmw.SameOrigin)

	// static
	fs := http.FileServer(http.Dir("web/static"))
	r.Handle("/static/*", http.StripPrefix("/static/", fs))
	r.Get("/sw.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Header().Set("Service-Worker-Allowed", "/")
		http.ServeFile(w, r, "web/static/sw.js")
	})
	r.Get("/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/manifest+json")
		http.ServeFile(w, r, "web/static/manifest.json")
	})

	// public
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := sessions.UserID(r); ok {
			http.Redirect(w, r, "/home", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})
	r.Get("/login", app.LoginPage)
	r.Post("/login", app.LoginSubmit)
	r.Post("/logout", app.Logout)
	r.Get("/register", app.RegisterPage)
	r.Post("/register", app.RegisterSubmit)

	// authenticated
	r.Group(func(r chi.Router) {
		r.Use(authmw.RequireAuth(sessions))

		r.Get("/home", app.HomePage)
		r.Get("/todo", app.TodoListPage)
		r.Get("/chat", app.ChatPage)
		r.Get("/settings", app.SettingsPage)

		// backward compat
		r.Get("/dashboard", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/home", http.StatusMovedPermanently)
		})

		r.Post("/tasks", app.TaskCreate)
		r.Post("/tasks/bulk", app.TaskBulk)
		r.Get("/tasks/{id}", app.TaskView)
		r.Post("/tasks/{id}/edit", app.TaskEdit)
		r.Post("/tasks/{id}/subtasks", app.TaskCreateSubtask)
		r.Post("/tasks/{id}/status", app.TaskUpdateStatus)
		r.Post("/tasks/{id}/project", app.TaskSetProject)
		r.Post("/tasks/{id}/delete", app.TaskDelete)
		r.Post("/tasks/{id}/archive", app.TaskArchive())
		r.Post("/tasks/{id}/unarchive", app.TaskUnarchive())
		r.Get("/archive", app.ArchivePage)

		r.Post("/projects", app.ProjectCreate)
		r.Post("/projects/{id}/rename", app.ProjectRename)
		r.Post("/projects/{id}/delete", app.ProjectDelete)

		r.Post("/account/username", app.AccountUsername)
		r.Post("/account/email", app.AccountEmail)
		r.Post("/account/password", app.AccountPassword)

		r.Group(func(r chi.Router) {
			r.Use(authmw.RequireAdmin(pool))
			r.Get("/admin/users", app.AdminUsers)
			r.Post("/admin/users/{id}/approve", app.AdminApprove())
			r.Post("/admin/users/{id}/reject", app.AdminReject())
		})

		r.Post("/prefs/theme", app.PrefsSetTheme)
		r.Post("/prefs/font", app.PrefsSetFont)

		r.Post("/chat", app.ChatSend)
		r.Post("/chat/clear", app.ChatClear)
		r.Post("/chat/{id}/delete", app.ChatDelete)
		r.Post("/chat/{id}/apply", app.ChatApply)
		r.Post("/chat/{id}/dismiss", app.ChatDismiss)
	})

	log.Printf("todo listening on :%s (provider=%s model=%s)", cfg.AppPort, aiClient.Provider(), aiClient.Model())
	if err := http.ListenAndServe(":"+cfg.AppPort, r); err != nil {
		log.Fatal(err)
	}
}

// bootstrapAdmin guarantees there is at least one admin.
// It does nothing if an admin already exists, so renaming the admin or changing its password
// through the Setting page survives restarts and never resurrects an account from .env.
// Otherwise it promotes the user named ADMIN_USERNAME (pre-multi-user databases), or creates it.
func bootstrapAdmin(ctx context.Context, db *sql.DB, username, password string) error {
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE is_admin AND status = 'approved'`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO users (username, password_hash) VALUES ($1, $2)
		ON CONFLICT DO NOTHING`, username, string(hash)); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx,
		`UPDATE users SET is_admin = true, status = 'approved' WHERE lower(username) = lower($1)`, username)
	return err
}
