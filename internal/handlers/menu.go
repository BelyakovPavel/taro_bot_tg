package handlers

import (
	"context"

	"github.com/go-telegram/bot"
	tgmodels "github.com/go-telegram/bot/models"
)

const (
	// BtnTarot and BtnNumerology are the main menu buttons. The texts
	// must match the registered handler patterns exactly.
	BtnTarot      = "Карты Таро"
	BtnNumerology = "Нумерология"
	// BtnBack returns from a subview to the main menu. It must match the
	// registered handler pattern exactly.
	BtnBack = "← Назад"
	// BtnClarifier draws an additional clarifier card for the last spread.
	BtnClarifier = "Пояснительная карта"
)

// Tarot handles the "Карты Таро" button press: it asks the user to
// type their question, then draws three cards.
func (h *Handler) Tarot(ctx context.Context, b *bot.Bot, update *tgmodels.Update) {
	if update.Message == nil {
		return
	}

	chatID := update.Message.Chat.ID
	h.setPending(chatID, pendingTarotQuery)

	h.sendSubview(ctx, b, chatID,
		"🔮 Карты Таро\n\nНапишите ваш запрос текстом, например:\n«Что меня ждёт в ближайший месяц?»\n\nЯ вытяну для вас 3 карты.")
}
