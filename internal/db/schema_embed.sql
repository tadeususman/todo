-- Idempotent schema — safe to run on every boot (mirrors journalflow pattern).

CREATE TABLE IF NOT EXISTS users (
    id            BIGSERIAL PRIMARY KEY,
    username      TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Multi-user: pendaftar baru berstatus 'pending' sampai disetujui admin. User lama otomatis 'approved'.
ALTER TABLE users ADD COLUMN IF NOT EXISTS status   TEXT    NOT NULL DEFAULT 'approved';  -- pending | approved | rejected
ALTER TABLE users ADD COLUMN IF NOT EXISTS is_admin BOOLEAN NOT NULL DEFAULT false;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username_lower ON users (lower(username));
ALTER TABLE users ADD COLUMN IF NOT EXISTS email TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_lower ON users (lower(email)) WHERE email IS NOT NULL;

CREATE TABLE IF NOT EXISTS sessions (
    id         TEXT PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);

CREATE TABLE IF NOT EXISTS user_prefs (
    user_id    BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    theme      TEXT NOT NULL DEFAULT 'system',   -- system | light | dark
    ui_font    TEXT NOT NULL DEFAULT 'system',   -- system | inter | jakarta | geist
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS projects (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    color      TEXT NOT NULL DEFAULT '#64748b',
    is_default BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, name)
);
CREATE INDEX IF NOT EXISTS idx_projects_user ON projects(user_id);

CREATE TABLE IF NOT EXISTS tasks (
    id                BIGSERIAL PRIMARY KEY,
    user_id           BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    parent_id         BIGINT REFERENCES tasks(id) ON DELETE CASCADE,
    project_id        BIGINT REFERENCES projects(id) ON DELETE SET NULL,
    title             TEXT NOT NULL,
    description       TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'pending',    -- pending | in_progress | done | cancelled
    priority          TEXT NOT NULL DEFAULT 'medium',     -- low | medium | high | urgent
    complexity        TEXT NOT NULL DEFAULT 'simple',     -- simple | medium | complex
    deadline          TIMESTAMPTZ,
    estimated_minutes INT,
    completed_at      TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Backfill for existing deployments where tasks table predates projects.
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS project_id BIGINT REFERENCES projects(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_tasks_user_status ON tasks(user_id, status);
CREATE INDEX IF NOT EXISTS idx_tasks_user_deadline ON tasks(user_id, deadline);
-- Arsip: task disembunyikan dari daftar tanpa dihapus; status aslinya tetap utuh.
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_tasks_parent ON tasks(parent_id);
CREATE INDEX IF NOT EXISTS idx_tasks_project ON tasks(project_id);

CREATE TABLE IF NOT EXISTS reminders (
    id         BIGSERIAL PRIMARY KEY,
    task_id    BIGINT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    remind_at  TIMESTAMPTZ NOT NULL,
    sent_at    TIMESTAMPTZ,
    channel    TEXT NOT NULL DEFAULT 'push',             -- push | telegram
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_reminders_due ON reminders(remind_at) WHERE sent_at IS NULL;

CREATE TABLE IF NOT EXISTS push_subscriptions (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    endpoint   TEXT NOT NULL UNIQUE,
    p256dh     TEXT NOT NULL,
    auth       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS chat_messages (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       TEXT NOT NULL,                      -- user | assistant
    content    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_chat_user_time ON chat_messages(user_id, created_at);
-- Perubahan task yang diusulkan asisten (JSON), menunggu konfirmasi user: pending | applied | dismissed.
ALTER TABLE chat_messages ADD COLUMN IF NOT EXISTS actions       TEXT;
ALTER TABLE chat_messages ADD COLUMN IF NOT EXISTS action_status TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS ai_logs (
    id            BIGSERIAL PRIMARY KEY,
    task_id       BIGINT REFERENCES tasks(id) ON DELETE SET NULL,
    provider      TEXT NOT NULL,
    model         TEXT NOT NULL,
    action        TEXT NOT NULL,                         -- parse | breakdown | reschedule
    input         TEXT NOT NULL,
    output        TEXT NOT NULL,
    tokens_in     INT,
    tokens_out    INT,
    error_msg     TEXT,
    duration_ms   INT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_ai_logs_created ON ai_logs(created_at DESC);
