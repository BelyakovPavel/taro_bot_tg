package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseBool(t *testing.T) {
	for _, s := range []string{"1", "true", "TRUE", "Yes", "on", " ON "} {
		if !parseBool(s) {
			t.Errorf("parseBool(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "0", "false", "no", "off", "2", "y"} {
		if parseBool(s) {
			t.Errorf("parseBool(%q) = true, want false", s)
		}
	}
}

// Load reads variables from the environment. godotenv.Load() is skipped
// by pointing HOME at an empty directory so a stray .env file elsewhere
// cannot leak in.
func TestLoadFromEnvironment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	t.Setenv("BOT_TOKEN", "123:abc")
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/db")
	t.Setenv("AI_API_KEY", "key")
	t.Setenv("PAYMENT_PROVIDER", "yukassa")
	t.Setenv("TEST_MODE", "1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BotToken != "123:abc" {
		t.Errorf("BotToken = %q", cfg.BotToken)
	}
	if cfg.DatabaseURL != "postgres://u:p@localhost:5432/db" {
		t.Errorf("DatabaseURL = %q", cfg.DatabaseURL)
	}
	if cfg.AIAPIKey != "key" || cfg.PaymentProvider != "yukassa" {
		t.Errorf("AIAPIKey/PaymentProvider = %q/%q", cfg.AIAPIKey, cfg.PaymentProvider)
	}
	if !cfg.TestMode {
		t.Errorf("TestMode = false, want true")
	}
}

func TestLoadFromDotEnv(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := "BOT_TOKEN=token_from_file\nDATABASE_URL=postgres://from:file@host/db\nTEST_MODE=yes\n"
	if err := os.WriteFile(envFile, []byte(content), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	// godotenv.Load() uses the process working directory; chdir into the
	// temp dir so the file is found.
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	// Environment variables take precedence over the .env file.
	t.Setenv("BOT_TOKEN", "env_wins")

	// DATABASE_URL must be absent from the environment so the .env file
	// value is applied (godotenv does not override existing variables).
	if old, ok := os.LookupEnv("DATABASE_URL"); ok {
		os.Unsetenv("DATABASE_URL")
		t.Cleanup(func() { os.Setenv("DATABASE_URL", old) })
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BotToken != "env_wins" {
		t.Errorf("BotToken = %q, want env value to win over .env", cfg.BotToken)
	}
	if cfg.DatabaseURL != "postgres://from:file@host/db" {
		t.Errorf("DatabaseURL = %q, want value from .env", cfg.DatabaseURL)
	}
	if !cfg.TestMode {
		t.Errorf("TestMode = false, want true from .env")
	}
}

func TestLoadRequiresTokenAndDatabaseURL(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	t.Setenv("BOT_TOKEN", "")
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/db")
	if _, err := Load(); err == nil {
		t.Errorf("Load without BOT_TOKEN: expected error")
	}

	t.Setenv("BOT_TOKEN", "123:abc")
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Errorf("Load without DATABASE_URL: expected error")
	}
}
