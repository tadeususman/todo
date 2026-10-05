package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"

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
	if err := pool.QueryRowContext(ctx, `SELECT id FROM users WHERE username = $1`, cfg.AdminUsername).Scan(&adminID); err != nil {
		log.Fatalf("locate admin: %v", err)
	}
	if err := project.EnsureDefault(ctx, pool, adminID); err != nil {
		log.Fatalf("seed default project: %v", err)
	}
	if err := prefs.EnsureRow(ctx, pool, adminID); err != nil {
		log.Fatalf("seed prefs: %v", err)
	}

	sessions := session.NewManager(pool)
	aiClient := llm.New(llm.Config{
		Provider:    cfg.AIProvider,
		BridgeURL:   cfg.BridgeURL,
		BridgeModel: cfg.BridgeModel,
		GeminiKey:   cfg.GeminiKey,
		GeminiModel: cfg.GeminiModel,
	}, pool)
	tmpl := handlers.LoadTemplates("web/templates")

	app := &handlers.App{
		DB:       pool,
		Sessions: sessions,
		LLM:      aiClient,
		Tmpl:     tmpl,
	}

	r := chi.NewRouter()
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)

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
		r.Get("/tasks/{id}", app.TaskView)
		r.Post("/tasks/{id}/edit", app.TaskEdit)
		r.Post("/tasks/{id}/subtasks", app.TaskCreateSubtask)
		r.Post("/tasks/{id}/status", app.TaskUpdateStatus)
		r.Post("/tasks/{id}/project", app.TaskSetProject)
		r.Post("/tasks/{id}/delete", app.TaskDelete)

		r.Post("/projects", app.ProjectCreate)
		r.Post("/projects/{id}/rename", app.ProjectRename)
		r.Post("/projects/{id}/delete", app.ProjectDelete)

		r.Post("/prefs/theme", app.PrefsSetTheme)
		r.Post("/prefs/font", app.PrefsSetFont)

		r.Post("/chat", app.ChatSend)
		r.Post("/chat/clear", app.ChatClear)
		r.Post("/chat/{id}/delete", app.ChatDelete)
	})

	log.Printf("todo listening on :%s (provider=%s model=%s)", cfg.AppPort, aiClient.Provider(), aiClient.Model())
	if err := http.ListenAndServe(":"+cfg.AppPort, r); err != nil {
		log.Fatal(err)
	}
}

// bootstrapAdmin ensures the single admin user exists.
// If missing OR the stored hash doesn't match the env password, upsert a fresh bcrypt hash.
func bootstrapAdmin(ctx context.Context, db *sql.DB, username, password string) error {
	var existingHash string
	err := db.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE username = $1`, username).Scan(&existingHash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if existingHash != "" && bcrypt.CompareHashAndPassword([]byte(existingHash), []byte(password)) == nil {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO users (username, password_hash) VALUES ($1, $2)
		ON CONFLICT (username) DO UPDATE SET password_hash = EXCLUDED.password_hash`,
		username, string(hash))
	return err
}
