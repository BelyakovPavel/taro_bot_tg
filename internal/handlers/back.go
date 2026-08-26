package handlers

import (
	"context"

	"github.com/go-telegram/bot"
	tgmodels "github.com/go-telegram/bot/models"
)

// Back handles the "← Назад" button: it cancels any pending input and
// returns the user to the main menu.
func (h *Handler) Back(ctx context.Context, b *bot.Bot, update *tgmodels.Update) {
	if update.Message == nil {
		return
	}

	chatID := update.Message.Chat.ID

	// Cancel a pending input (e.g. a tarot query the bot was waiting for).
	h.takePending(chatID)

	h.sendMenu(ctx, b, chatID, "Выберите раздел:")
}
