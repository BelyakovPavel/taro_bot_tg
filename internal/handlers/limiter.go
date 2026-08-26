package handlers

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/go-telegram/bot"
	tgmodels "github.com/go-telegram/bot/models"
)

// Rate limiting configuration. The limiter is a per-user token bucket:
// every incoming message from a user consumes one token, tokens refill
// at rateLimitPerSec per second, and a user may burst up to
// rateLimitBurst tokens at once. This protects the bot from spam,
// bot attacks and overloads (database and AI API costs).
const (
	rateLimitPerSec      = 5.0
	rateLimitBurst       = 20.0
	rateLimitNotifyEvery = 30 * time.Second // max frequency of throttling notices per user
	rateLimitSweepAfter  = 4096             // prune idle buckets once the map grows beyond this
	rateLimitIdleTimeout = 2 * time.Minute  // bucket is removed when idle longer than this
)

// userBucket holds the token balance of a single user.
type userBucket struct {
	tokens  float64
	last    time.Time // last refill/consumption
	lastLog time.Time // last throttling notice sent
}

// userLimiter is a per-user token bucket, safe for concurrent use.
type userLimiter struct {
	mu      sync.Mutex
	buckets map[int64]*userBucket
	rate    float64 // tokens refilled per second
	burst   float64 // maximum token capacity
}

func newUserLimiter(rate, burst float64) *userLimiter {
	return &userLimiter{
		buckets: make(map[int64]*userBucket),
		rate:    rate,
		burst:   burst,
	}
}

// allow consumes one token for the user and reports whether the request
// may proceed. Idle buckets are pruned occasionally to keep memory
// bounded.
func (l *userLimiter) allow(userID int64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	if len(l.buckets) > rateLimitSweepAfter {
		l.sweepLocked(now)
	}

	b, ok := l.buckets[userID]
	if !ok {
		b = &userBucket{tokens: l.burst, last: now}
		l.buckets[userID] = b
	}

	b.tokens += now.Sub(b.last).Seconds() * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// shouldNotify reports whether a throttling notice may be sent to the
// user now. It returns true at most once per rateLimitNotifyEvery.
func (l *userLimiter) shouldNotify(userID int64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	b, ok := l.buckets[userID]
	if !ok {
		// First notice for this user: record it and allow.
		l.buckets[userID] = &userBucket{tokens: l.burst, last: now, lastLog: now}
		return true
	}
	if now.Sub(b.lastLog) >= rateLimitNotifyEvery {
		b.lastLog = now
		return true
	}
	return false
}

// sweepLocked deletes buckets that have been idle for longer than
// rateLimitIdleTimeout. Callers must hold l.mu.
func (l *userLimiter) sweepLocked(now time.Time) {
	for id, b := range l.buckets {
		if now.Sub(b.last) > rateLimitIdleTimeout {
			delete(l.buckets, id)
		}
	}
}

// RateLimitMiddleware returns a bot middleware that enforces the
// per-user message rate limit. Updates without a sender (e.g. channel
// posts) pass through. Throttled users get a short notice at most once
// per rateLimitNotifyEvery.
func (h *Handler) RateLimitMiddleware() bot.Middleware {
	return func(next bot.HandlerFunc) bot.HandlerFunc {
		return func(ctx context.Context, b *bot.Bot, update *tgmodels.Update) {
			if update.Message == nil || update.Message.From == nil {
				next(ctx, b, update)
				return
			}

			userID := update.Message.From.ID
			if h.limiter.allow(userID) {
				next(ctx, b, update)
				return
			}

			log.Printf("rate limit: dropping message from user %d", userID)
			if h.limiter.shouldNotify(userID) {
				_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
					ChatID: update.Message.Chat.ID,
					Text:   "⏳ Слишком много сообщений. Подождите немного и повторите.",
				})
			}
		}
	}
}
