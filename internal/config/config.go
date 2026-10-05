package config

import (
	"log"
	"os"
)

const Version = "0.1.0"

type Config struct {
	AppPort       string
	DBUrl         string
	SessionSecret string
	TZ            string

	AdminUsername string
	AdminPassword string

	AIProvider   string
	BridgeURL    string
	BridgeModel  string
	GeminiKey    string
	GeminiModel  string
}

func Load() Config {
	return Config{
		AppPort:       getEnv("APP_PORT", "8080"),
		DBUrl:         mustEnv("DATABASE_URL"),
		SessionSecret: mustEnv("SESSION_SECRET"),
		TZ:            getEnv("TZ", "Asia/Jakarta"),

		AdminUsername: getEnv("ADMIN_USERNAME", "tade"),
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
