package handlers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"strings"

	"github.com/go-telegram/bot"
	tgmodels "github.com/go-telegram/bot/models"

	"taro_bot/internal/payment"
	"taro_bot/internal/storage"
)

// tarotSystemPrompt instructs the AI how to write the prediction.
const tarotSystemPrompt = `Ты — опытный таролог-консультант. Ты получишь вопрос пользователя и три выпавшие карты Таро с ключевыми словами и кратким значением. Дай развёрнутый, тёплый и конкретный прогноз на русском языке из 6–10 предложений: короткое вступление, значение каждой карты в контексте вопроса пользователя и в конце практический совет. Не упоминай, что ты искусственный интеллект, и не используй markdown-разметку.`

// handleTarotQuery processes a user's tarot request: it stores the
// question (bound to the user's uid) with the drawn cards in the
// database, replies that the spread is being made, and delivers the
// result in a separate message after a random delay, followed by an
// AI-generated prediction.
func (h *Handler) handleTarotQuery(ctx context.Context, b *bot.Bot, update *tgmodels.Update) {
	msg := update.Message
	chatID := msg.Chat.ID

	if msg.From == nil {
		return
	}

	user, err := h.store.GetUserByTelegramID(ctx, msg.From.ID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			h.sendText(ctx, b, chatID, "Сначала нажмите /start, чтобы зарегистрироваться.")
			return
		}
		log.Printf("get user %d: %v", msg.From.ID, err)
		h.sendText(ctx, b, chatID, "Произошла ошибка. Попробуйте позже.")
		return
	}

	if err := h.requireFeature(ctx, b, chatID, user, payment.FeatureTarotSpread); err != nil {
		return
	}

	query := strings.TrimSpace(msg.Text)
	if tooLong(query, maxTarotQueryLen) {
		h.sendText(ctx, b, chatID,
			fmt.Sprintf("Запрос слишком длинный — максимум %d символов. Попробуйте сформулировать короче.", maxTarotQueryLen))
		return
	}

	cards := drawCards(3)

	// The request is persisted together with the drawn cards; the same
	// data later goes into the AI prompt. The clarifier card and both AI
	// answers are filled into this same row as they are produced, and
	// the whole row is cleaned up by the 48h TTL job.
	requestID, err := h.store.SaveTarotRequest(ctx, user.UID, query, cards)
	if err != nil {
		log.Printf("save tarot request for %s: %v", user.UID, err)
		h.sendText(ctx, b, chatID, "Не удалось сохранить запрос. Попробуйте позже.")
		return
	}

	h.sendText(ctx, b, chatID, fmt.Sprintf(
		"🔮 Принято! Делаю расклад на ваш вопрос: «%s»\n\n"+
			"Это займёт от 1 до 3 минут. Я напишу, когда карты будут готовы.",
		query,
	))

	h.schedule(ctx, h.delays.TarotMin, h.delays.TarotMax, func() {
		h.sendTarotSpread(ctx, b, chatID, requestID, query, cards)
	})
}

// sendTarotSpread delivers two consecutive messages: the drawn cards
// (names, keywords and meanings from the database) and, right after, the
// AI-generated prediction for the user's question. The prediction is
// stored on the request row.
func (h *Handler) sendTarotSpread(ctx context.Context, b *bot.Bot, chatID int64, requestID int64, query string, cards []int) {
	cardLines, aiLines := h.formatCards(ctx, cards)

	if len(cardLines) == 0 {
		h.sendText(ctx, b, chatID, "Не удалось получить карты. Попробуйте позже.")
		return
	}

	// Message 1: the spread itself.
	h.sendText(ctx, b, chatID, fmt.Sprintf(
		"🃏 Ваш расклад готов!\n\nЗапрос: «%s»\n\n%s",
		query, strings.Join(cardLines, "\n\n"),
	))

	// Message 2: the AI prediction.
	userContent := fmt.Sprintf(
		"Вопрос пользователя: «%s»\n\nВыпавшие карты:\n%s",
		query, strings.Join(aiLines, "\n"),
	)

	prediction, err := h.ai.Generate(ctx, tarotSystemPrompt, userContent)
	if err != nil {
		log.Printf("ai prediction: %v", err)
		h.sendText(ctx, b, chatID, "Не удалось получить прогноз от ИИ. Попробуйте позже ещё раз.")
		h.sendPostSpreadMenu(ctx, b, chatID, "Выберите действие:")
		return
	}

	if err := h.store.UpdateTarotRequestAnswer(ctx, requestID, prediction); err != nil {
		log.Printf("save tarot answer for request %d: %v", requestID, err)
	}

	h.sendText(ctx, b, chatID, "🔮 Прогноз:\n\n"+prediction)
	h.sendPostSpreadMenu(ctx, b, chatID, "Выберите действие:")
}

// drawCards returns count unique random numbers in the range 1..78,
// like drawing cards from a shuffled deck without replacement.
func drawCards(count int) []int {
	perm := rand.Perm(78) // shuffled 0..77
	cards := make([]int, count)
	for i := 0; i < count; i++ {
		cards[i] = perm[i] + 1
	}
	return cards
}
