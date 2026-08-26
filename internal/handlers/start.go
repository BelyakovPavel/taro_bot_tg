package handlers

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/go-telegram/bot"
	tgmodels "github.com/go-telegram/bot/models"
	"github.com/google/uuid"

	"taro_bot/internal/models"
	"taro_bot/internal/storage"
)

// Start registers a new user on /start: it generates a uid, saves the
// Telegram profile (login, first/last name, telegram id) to the database
// and asks for the phone number via a contact button. Returning users
// are greeted and shown their saved uid.
func (h *Handler) Start(ctx context.Context, b *bot.Bot, update *tgmodels.Update) {
	if update.Message == nil || update.Message.From == nil {
		return
	}

	from := update.Message.From

	user, err := h.store.GetUserByTelegramID(ctx, from.ID)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		log.Printf("get user %d: %v", from.ID, err)
		h.sendText(ctx, b, from.ID, "Произошла ошибка. Попробуйте позже.")
		return
	}

	if user != nil {
		text := fmt.Sprintf("С возвращением, %s!\nВаш UID: %s", displayName(user), user.UID)
		if user.Phone == "" {
			text += "\n\nНомер телефона ещё не указан — нажмите кнопку ниже, чтобы поделиться им."
			h.sendPhoneRequest(ctx, b, from.ID, text)
			return
		}
		h.sendMenu(ctx, b, from.ID, text+"\n\nВыберите раздел:")
		return
	}

	// New user: create uid and persist the profile.
	user = &models.User{
		UID:        uuid.NewString(),
		TelegramID: from.ID,
		Username:   from.Username,
		FirstName:  from.FirstName,
		LastName:   from.LastName,
	}
	if err := h.store.SaveUser(ctx, user); err != nil {
		log.Printf("save user %d: %v", from.ID, err)
		h.sendText(ctx, b, from.ID, "Не удалось сохранить данные. Попробуйте позже.")
		return
	}

	log.Printf("new user registered: uid=%s telegram_id=%d username=%q",
		user.UID, user.TelegramID, user.Username)

	text := fmt.Sprintf("Привет, %s!\nЯ сохранил вас в базе.\nUID: %s\n\n"+
		"Остался один шаг — поделитесь номером телефона по кнопке ниже.",
		displayName(user), user.UID)
	h.sendPhoneRequest(ctx, b, from.ID, text)
}
