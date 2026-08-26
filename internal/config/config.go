// Package config loads the bot configuration from the .env file
// and environment variables.
package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds all application settings.
type Config struct {
	// BotToken is the Telegram bot token obtained from @BotFather.
	BotToken string
	// DatabaseURL is the PostgreSQL connection string, e.g.
	// postgres://user:pass@host:5432/db?sslmode=disable.
	DatabaseURL string
	// AIAPIKey is the DeepSeek API key used to generate predictions.
	// Optional: if empty, the AI features reply with an error message.
	AIAPIKey string
	// PaymentProvider selects the online payment gateway. Empty means
	// payments are not configured and the bot runs in free mode.
	PaymentProvider string
	// TestMode shortens feature waits for development: numerology results
	// are delivered immediately instead of after a random 1-2 hours.
	TestMode bool
}

// Load reads configuration from the .env file (via godotenv) and the
// environment. Missing .env file is not fatal — variables may be set
// externally — but BOT_TOKEN and DATABASE_URL must be present.
func Load() (*Config, error) {
	if err := godotenv.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: .env file not loaded: %v\n", err)
	}

	cfg := &Config{
		BotToken:        os.Getenv("BOT_TOKEN"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		AIAPIKey:        os.Getenv("AI_API_KEY"),
		PaymentProvider: os.Getenv("PAYMENT_PROVIDER"),
		TestMode:        parseBool(os.Getenv("TEST_MODE")),
	}

	if cfg.BotToken == "" {
		return nil, fmt.Errorf("BOT_TOKEN is not set (add it to .env or export it)")
	}
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is not set (add it to .env or export it)")
	}

	return cfg, nil
}

// parseBool interprets common boolean representations ("1", "true",
// "yes", "on") case-insensitively. Anything else is false.
func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
