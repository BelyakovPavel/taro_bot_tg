package handlers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/go-telegram/bot"
	tgmodels "github.com/go-telegram/bot/models"

	"taro_bot/internal/models"
	"taro_bot/internal/payment"
	"taro_bot/internal/storage"
)

// clarifierSystemPrompt instructs the AI how to write the combined
// explanation of the main cards plus the clarifier card.
const clarifierSystemPrompt = `Ты — опытный таролог-консультант. Пользователь задал вопрос, выпали три основные карты Таро и одна пояснительная карта, которая уточняет и дополняет расклад. Дай итоговое объяснение на русском языке из 8–12 предложений: короткое вступление, разбор каждой основной карты в контексте вопроса, объяснение, что добавляет пояснительная карта, и практический совет в конце. Не упоминай, что ты искусственный интеллект, и не используй markdown-разметку.`

// Clarifier handles the "Пояснительная карта" button: it draws one extra
// card for the latest tarot request stored in the database and, after a
// random delay, delivers the card followed by an AI explanation that
// combines the main cards and the clarifier card. Both the card and the
// explanation are saved on the request row.
func (h *Handler) Clarifier(ctx context.Context, b *bot.Bot, update *tgmodels.Update) {
	if update.Message == nil || update.Message.From == nil {
		return
	}
	chatID := update.Message.Chat.ID

	user, err := h.store.GetUserByTelegramID(ctx, update.Message.From.ID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			h.sendText(ctx, b, chatID, "Сначала нажмите /start, чтобы зарегистрироваться.")
			return
		}
		log.Printf("get user %d: %v", update.Message.From.ID, err)
		h.sendText(ctx, b, chatID, "Произошла ошибка. Попробуйте позже.")
		return
	}

	if err := h.requireFeature(ctx, b, chatID, user, payment.FeatureTarotClarifier); err != nil {
		return
	}

	req, err := h.store.GetLatestTarotRequest(ctx, user.UID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			h.sendText(ctx, b, chatID, "Сначала сделайте расклад — нажмите «Карты Таро».")
			h.sendMenu(ctx, b, chatID, "Выберите раздел:")
			return
		}
		log.Printf("get latest tarot request for %s: %v", user.UID, err)
		h.sendText(ctx, b, chatID, "Произошла ошибка. Попробуйте позже.")
		return
	}

	// Only one clarifier card per request.
	if req.ClarifierCard != 0 {
		h.sendText(ctx, b, chatID, "Пояснительная карта для этого расклада уже вытянута. Сделайте новый расклад, чтобы получить ещё одну.")
		h.sendMenu(ctx, b, chatID, "Выберите раздел:")
		return
	}

	cardID := drawCards(1)[0]

	// The clarifier card is saved immediately so the button disappears
	// for this request, even if the user presses it again while the
	// explanation is still pending.
	if err := h.store.UpdateTarotRequestClarifier(ctx, req.ID, cardID); err != nil {
		log.Printf("save clarifier card for request %d: %v", req.ID, err)
		h.sendText(ctx, b, chatID, "Не удалось сохранить карту. Попробуйте позже.")
		return
	}

	h.sendText(ctx, b, chatID,
		"✨ Принято! Вытягиваю пояснительную карту к вашему раскладу.\n\n"+
			"Это займёт от 30 секунд до полутора минут.")

	h.schedule(ctx, h.delays.ClarifierMin, h.delays.ClarifierMax, func() {
		h.sendClarifierResult(ctx, b, chatID, req, cardID)
	})
}

// sendClarifierResult delivers the clarifier card and the combined
// AI explanation as two consecutive messages. The explanation is stored
// on the request row.
func (h *Handler) sendClarifierResult(ctx context.Context, b *bot.Bot, chatID int64, req *models.TarotRequest, cardID int) {
	card, err := h.store.GetTarotCard(ctx, cardID)
	if err != nil {
		log.Printf("get clarifier card %d: %v", cardID, err)
		h.sendText(ctx, b, chatID, "Не удалось получить карту. Попробуйте позже.")
		h.sendMenu(ctx, b, chatID, "Выберите раздел:")
		return
	}

	// Message 1: the clarifier card itself.
	h.sendText(ctx, b, chatID, fmt.Sprintf(
		"✨ Пояснительная карта:\n%s (№%d)\nКлючевые слова: %s\nЗначение: %s",
		card.Name, card.ID, card.Keywords, card.Description,
	))

	// Message 2: combined AI explanation (main cards + clarifier).
	_, mainLines := h.formatCards(ctx, req.Cards)

	userContent := fmt.Sprintf(
		"Вопрос пользователя: «%s»\n\nОсновные карты:\n%s\n\nПояснительная карта:\n%s (№%d) — ключевые слова: %s; значение: %s",
		req.Request,
		strings.Join(mainLines, "\n"),
		card.Name, card.ID, card.Keywords, card.Description,
	)

	prediction, err := h.ai.Generate(ctx, clarifierSystemPrompt, userContent)
	if err != nil {
		log.Printf("ai clarifier prediction: %v", err)
		h.sendText(ctx, b, chatID, "Не удалось получить итоговое объяснение от ИИ. Попробуйте позже ещё раз.")
		h.sendMenu(ctx, b, chatID, "Выберите раздел:")
		return
	}

	if err := h.store.UpdateTarotRequestClarifierAnswer(ctx, req.ID, prediction); err != nil {
		log.Printf("save clarifier answer for request %d: %v", req.ID, err)
	}

	h.sendText(ctx, b, chatID, "🔮 Итоговое объяснение:\n\n"+prediction)
	// The clarifier card is spent: show the plain menu without the button.
	h.sendMenu(ctx, b, chatID, "Выберите раздел:")
}
