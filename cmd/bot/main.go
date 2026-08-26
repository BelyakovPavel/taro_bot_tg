package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/go-telegram/bot"

	"taro_bot/internal/ai"
	"taro_bot/internal/config"
	"taro_bot/internal/handlers"
	"taro_bot/internal/payment"
	"taro_bot/internal/storage"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	store, err := storage.Open(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}
	defer store.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	go runDataCleanup(ctx, store)

	if cfg.AIAPIKey == "" {
		log.Println("warning: AI_API_KEY is not set — AI predictions will not work")
	}
	if cfg.PaymentProvider != "" {
		log.Printf("warning: payment provider %q is not implemented yet — running in free mode", cfg.PaymentProvider)
	}

	h := handlers.New(store, ai.NewClient(cfg.AIAPIKey), payment.New(nil), handlers.DefaultDelays(), cfg.TestMode)

	b, err := bot.New(cfg.BotToken,
		bot.WithDefaultHandler(h.Echo),
		bot.WithMiddlewares(h.RateLimitMiddleware()),
	)
	if err != nil {
		log.Fatalf("create bot: %v", err)
	}
	h.Register(b)

	log.Printf("bot started")
	b.Start(ctx)
}

// runDataCleanup deletes tarot requests older than the 48h TTL once at
// startup and then hourly until ctx is cancelled.
func runDataCleanup(ctx context.Context, store *storage.Storage) {
	cleanup := func() {
		if err := store.CleanupTarotRequests(ctx); err != nil {
			log.Printf("cleanup tarot requests: %v", err)
		}
	}
	cleanup()

	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			cleanup()
		case <-ctx.Done():
			return
		}
	}
}
