package config

import (
	"log"
	"os"
	"strconv"
	"strings"
)

const Version = "0.1.0"

type Config struct {
	AppPort            string
	DBUrl              string
	PublicURL          string // https://todo.example.com, used in emailed links (never derived from the Host header)
	SMTPHost, SMTPPort string
	SMTPUser, SMTPPass string
	SMTPFrom           string
	TelegramBotToken   string // optional: admin alerts (new sign-ups)
	TelegramChatID     string
	SupportEmail       string // contact shown on the public privacy / delete-account pages
	AndroidPackage     string // Play Store package name for /.well-known/assetlinks.json
	AndroidSHA256      string // comma-separated SHA-256 signing-cert fingerprints
	AILogRetentionDays int    // ai_logs older than this are purged
	TZ                 string
	TrustedProxies     string // comma-separated CIDRs/IPs allowed to set X-Real-IP (empty = loopback+private)
	RedirectHosts      string // comma-separated old hostnames redirected to PublicURL

	AdminUsername string
	AdminPassword string

	AIProvider  string
	BridgeURL   string
	BridgeModel string
	GeminiKey   string
	GeminiModel string
}

func Load() Config {
	return Config{
		AppPort:            getEnv("APP_PORT", "8080"),
		DBUrl:              mustEnv("DATABASE_URL"),
		PublicURL:          strings.TrimRight(os.Getenv("PUBLIC_URL"), "/"),
		SMTPHost:           os.Getenv("SMTP_HOST"),
		SMTPPort:           getEnv("SMTP_PORT", "587"),
		SMTPUser:           os.Getenv("SMTP_USER"),
		SMTPPass:           os.Getenv("SMTP_PASS"),
		SMTPFrom:           os.Getenv("SMTP_FROM"),
		SupportEmail:       os.Getenv("SUPPORT_EMAIL"),
		TelegramBotToken:   os.Getenv("TELEGRAM_BOT_TOKEN"),
		TelegramChatID:     os.Getenv("TELEGRAM_CHAT_ID"),
		AndroidPackage:     os.Getenv("ANDROID_PACKAGE"),
		AndroidSHA256:      os.Getenv("ANDROID_SHA256"),
		AILogRetentionDays: getEnvInt("AI_LOG_RETENTION_DAYS", 30),
		TZ:                 getEnv("TZ", "Asia/Jakarta"),
		TrustedProxies:     os.Getenv("TRUSTED_PROXIES"),
		RedirectHosts:      os.Getenv("REDIRECT_HOSTS"),

		AdminUsername: mustEnv("ADMIN_USERNAME"),
		AdminPassword: mustEnv("ADMIN_PASSWORD"),

		AIProvider:  getEnv("AI_PROVIDER", "bridge"),
		BridgeURL:   getEnv("BRIDGE_URL", "http://host.docker.internal:8765"),
		BridgeModel: getEnv("BRIDGE_MODEL", "claude-sonnet-4-6"),
		GeminiKey:   os.Getenv("GEMINI_API_KEY"),
		GeminiModel: getEnv("GEMINI_MODEL", "gemini-2.0-flash"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("missing required environment variable: %s", key)
	}
	return v
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
		log.Fatalf("%s must be a positive integer", key)
	}
	return fallback
}
