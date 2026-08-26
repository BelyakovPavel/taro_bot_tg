// Package handlers contains Telegram bot update handlers.
package handlers

import (
	"context"
	"fmt"
	"log"
	"math/rand/v2"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-telegram/bot"
	tgmodels "github.com/go-telegram/bot/models"

	"taro_bot/internal/ai"
	"taro_bot/internal/models"
	"taro_bot/internal/payment"
	"taro_bot/internal/storage"
)

// pendingAction marks that a chat is waiting for a specific input.
type pendingAction string

const (
	// pendingTarotQuery: the bot asked the user to type a tarot request.
	pendingTarotQuery pendingAction = "tarot_query"
	// Numerology input steps, in order: date, time, place of birth.
	pendingNumerologyDate  pendingAction = "numerology_date"
	pendingNumerologyTime  pendingAction = "numerology_time"
	pendingNumerologyPlace pendingAction = "numerology_place"
)

// numerologyInput accumulates the birth data collected from the user
// across the three numerology steps.
type numerologyInput struct {
	date  string
	time  string
	place string
}

// pendingState is a pending action plus the data collected so far.
type pendingState struct {
	action     pendingAction
	numerology *numerologyInput
}

// Delays bounds the random waiting time before a result is delivered.
// Zero values deliver the result immediately (used by tests).
type Delays struct {
	TarotMin, TarotMax           time.Duration
	ClarifierMin, ClarifierMax   time.Duration
	NumerologyMin, NumerologyMax time.Duration
}

// DefaultDelays returns the production waiting times: 1-3 minutes for
// tarot spreads, 30-90 seconds for clarifier cards, 1-2 hours for
// numerology results.
func DefaultDelays() Delays {
	return Delays{
		TarotMin:      60 * time.Second,
		TarotMax:      180 * time.Second,
		ClarifierMin:  30 * time.Second,
		ClarifierMax:  90 * time.Second,
		NumerologyMin: 60 * time.Minute,
		NumerologyMax: 120 * time.Minute,
	}
}

// Input length limits (in runes) protecting the bot from oversized
// messages, oversized database rows and excessive AI API costs.
const (
	maxTarotQueryLen      = 300
	maxNumerologyPlaceLen = 100
	maxEchoTextLen        = 1000
)

// tooLong reports whether s exceeds max runes.
func tooLong(s string, max int) bool {
	return utf8.RuneCountInString(s) > max
}

// Handler holds dependencies shared by all Telegram handlers.
type Handler struct {
	store    *storage.Storage
	ai       *ai.Client
	payments *payment.Service
	delays   Delays
	testMode bool
	limiter  *userLimiter

	mu      sync.Mutex
	pending map[int64]pendingState // chat ID -> pending state
}

// New creates a Handler with the given storage, AI client, payment
// service, delivery delays and test-mode flag.
func New(store *storage.Storage, aiClient *ai.Client, payments *payment.Service, delays Delays, testMode bool) *Handler {
	return &Handler{
		store:    store,
		ai:       aiClient,
		payments: payments,
		delays:   delays,
		testMode: testMode,
		limiter:  newUserLimiter(rateLimitPerSec, rateLimitBurst),
		pending:  make(map[int64]pendingState),
	}
}

// Register wires all message handlers onto the bot. It is shared by the
// binary entry point and the smoke tests so both use identical wiring.
func (h *Handler) Register(b *bot.Bot) {
	b.RegisterHandler(bot.HandlerTypeMessageText, "/start", bot.MatchTypeExact, h.Start)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/privacy", bot.MatchTypeExact, h.Privacy)
	b.RegisterHandler(bot.HandlerTypeMessageText, BtnTarot, bot.MatchTypeExact, h.Tarot)
	b.RegisterHandler(bot.HandlerTypeMessageText, BtnNumerology, bot.MatchTypeExact, h.Numerology)
	b.RegisterHandler(bot.HandlerTypeMessageText, BtnBack, bot.MatchTypeExact, h.Back)
	b.RegisterHandler(bot.HandlerTypeMessageText, BtnClarifier, bot.MatchTypeExact, h.Clarifier)
}

// setPending marks the chat as waiting for the given input.
func (h *Handler) setPending(chatID int64, action pendingAction) {
	h.setPendingState(chatID, pendingState{action: action})
}

// setPendingState marks the chat as waiting for the given state,
// preserving any already collected input data.
func (h *Handler) setPendingState(chatID int64, st pendingState) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pending[chatID] = st
}

// takePending returns and clears the pending state for the chat.
func (h *Handler) takePending(chatID int64) (pendingState, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	st, ok := h.pending[chatID]
	if ok {
		delete(h.pending, chatID)
	}
	return st, ok
}

// requireFeature checks payment access for a feature and sends an
// explanatory message when a payment is required. It returns an error
// so the caller stops further processing.
func (h *Handler) requireFeature(ctx context.Context, b *bot.Bot, chatID int64, user *models.User, feature payment.Feature) error {
	pay, err := h.payments.Require(ctx, user.UID, feature)
	if err != nil {
		log.Printf("payment check for %s (%s): %v", user.UID, feature, err)
		h.sendText(ctx, b, chatID, "Не удалось проверить доступ к разделу. Попробуйте позже.")
		return err
	}
	if pay != nil {
		h.sendText(ctx, b, chatID, fmt.Sprintf("💳 Раздел платный. Оплатите по ссылке: %s", pay.PayURL))
		return payment.ErrPaymentRequired
	}
	return nil
}

// randomDelay picks a uniform random duration in [min, max]. It is safe
// to call with zero bounds (returns immediately).
func randomDelay(min, max time.Duration) time.Duration {
	if min >= max {
		return min
	}
	return min + time.Duration(rand.Int64N(int64(max-min+1)))
}

// schedule runs fn after a random delay in [min, max] unless ctx is
// cancelled first.
func (h *Handler) schedule(ctx context.Context, min, max time.Duration, fn func()) {
	delay := randomDelay(min, max)
	go func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return
		}
		fn()
	}()
}

func (h *Handler) sendText(ctx context.Context, b *bot.Bot, chatID int64, text string) {
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   text,
	})
}

// sendPhoneRequest shows a one-time keyboard with a "share contact"
// button so Telegram delivers the user's phone number to the bot.
func (h *Handler) sendPhoneRequest(ctx context.Context, b *bot.Bot, chatID int64, text string) {
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   text,
		ReplyMarkup: &tgmodels.ReplyKeyboardMarkup{
			Keyboard: [][]tgmodels.KeyboardButton{
				{{Text: "📱 Поделиться номером", RequestContact: true}},
			},
			ResizeKeyboard:  true,
			OneTimeKeyboard: true,
		},
	})
}

// sendMenu shows the main menu with the two feature buttons.
// Sending a new reply keyboard replaces any previous one.
func (h *Handler) sendMenu(ctx context.Context, b *bot.Bot, chatID int64, text string) {
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   text,
		ReplyMarkup: &tgmodels.ReplyKeyboardMarkup{
			Keyboard: [][]tgmodels.KeyboardButton{
				{
					{Text: BtnTarot},
					{Text: BtnNumerology},
				},
			},
			ResizeKeyboard: true,
		},
	})
}

// sendPostSpreadMenu shows the menu after a spread is delivered:
// a clarifier card button on top, then the main menu.
func (h *Handler) sendPostSpreadMenu(ctx context.Context, b *bot.Bot, chatID int64, text string) {
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   text,
		ReplyMarkup: &tgmodels.ReplyKeyboardMarkup{
			Keyboard: [][]tgmodels.KeyboardButton{
				{{Text: BtnClarifier}},
				{{Text: BtnTarot}, {Text: BtnNumerology}},
			},
			ResizeKeyboard: true,
		},
	})
}

// sendSubview shows a text with only the "back" button in the keyboard,
// e.g. inside a section the user entered from the main menu.
func (h *Handler) sendSubview(ctx context.Context, b *bot.Bot, chatID int64, text string) {
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   text,
		ReplyMarkup: &tgmodels.ReplyKeyboardMarkup{
			Keyboard: [][]tgmodels.KeyboardButton{
				{{Text: BtnBack}},
			},
			ResizeKeyboard: true,
		},
	})
}

// formatCards renders the cards with ids into two representations: user
// lines (formatted for the Telegram message) and AI lines (compact
// context lines for the prompt). Cards that cannot be loaded are skipped.
func (h *Handler) formatCards(ctx context.Context, ids []int) (userLines, aiLines []string) {
	for i, id := range ids {
		card, err := h.store.GetTarotCard(ctx, id)
		if err != nil {
			log.Printf("get tarot card %d: %v", id, err)
			continue
		}
		userLines = append(userLines, fmt.Sprintf(
			"%d. %s (№%d)\n   Ключевые слова: %s\n   Значение: %s",
			i+1, card.Name, card.ID, card.Keywords, card.Description,
		))
		aiLines = append(aiLines, fmt.Sprintf(
			"%d. %s (№%d) — ключевые слова: %s; значение: %s",
			i+1, card.Name, card.ID, card.Keywords, card.Description,
		))
	}
	return userLines, aiLines
}

// displayName builds a human-readable name from a user profile.
func displayName(u *models.User) string {
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if name == "" && u.Username != "" {
		return "@" + u.Username
	}
	if name == "" {
		return "пользователь"
	}
	return name
}
