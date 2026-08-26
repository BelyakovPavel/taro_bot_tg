package handlers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/go-telegram/bot"
	tgmodels "github.com/go-telegram/bot/models"
	"github.com/google/uuid"

	"taro_bot/internal/models"
	"taro_bot/internal/storage"
)

// Echo is the default handler. It processes pending actions (such as a
// tarot query the user was asked to type), saves the phone number when
// the user shares a contact, and echoes any other text back.
func (h *Handler) Echo(ctx context.Context, b *bot.Bot, update *tgmodels.Update) {
	if update.Message == nil {
		return
	}

	if update.Message.Contact != nil {
		h.handleContact(ctx, b, update)
		return
	}

	// A pending action means the user was asked for specific input
	// (e.g. a tarot request after pressing the "Карты Таро" button).
	if st, ok := h.takePending(update.Message.Chat.ID); ok {
		switch st.action {
		case pendingTarotQuery:
			if update.Message.Text == "" {
				h.setPending(update.Message.Chat.ID, pendingTarotQuery)
				h.sendText(ctx, b, update.Message.Chat.ID, "Пожалуйста, отправьте запрос текстом.")
				return
			}
			if tooLong(strings.TrimSpace(update.Message.Text), maxTarotQueryLen) {
				h.setPending(update.Message.Chat.ID, pendingTarotQuery)
				h.sendText(ctx, b, update.Message.Chat.ID,
					fmt.Sprintf("Запрос слишком длинный — максимум %d символов. Попробуйте сформулировать короче.", maxTarotQueryLen))
				return
			}
			h.handleTarotQuery(ctx, b, update)
			return
		case pendingNumerologyDate:
			h.handleNumerologyDate(ctx, b, update, st)
			return
		case pendingNumerologyTime:
			h.handleNumerologyTime(ctx, b, update, st)
			return
		case pendingNumerologyPlace:
			h.handleNumerologyPlace(ctx, b, update, st)
			return
		}
	}

	if update.Message.Text == "" {
		return
	}

	text := strings.TrimSpace(update.Message.Text)
	if tooLong(text, maxEchoTextLen) {
		h.sendText(ctx, b, update.Message.Chat.ID, "Сообщение слишком длинное — максимум 1000 символов.")
		return
	}

	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   text,
	})
}

// handleContact stores the phone number shared via the contact button.
func (h *Handler) handleContact(ctx context.Context, b *bot.Bot, update *tgmodels.Update) {
	msg := update.Message
	contact := msg.Contact

	telegramID := contact.UserID
	if telegramID == 0 && msg.From != nil {
		telegramID = msg.From.ID
	}
	if telegramID == 0 {
		log.Printf("contact without user id, ignoring")
		return
	}

	user, err := h.store.GetUserByTelegramID(ctx, telegramID)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		log.Printf("get user %d: %v", telegramID, err)
		h.sendText(ctx, b, msg.Chat.ID, "Произошла ошибка. Попробуйте позже.")
		return
	}

	if user == nil {
		// Contact from a user who never pressed /start: register them
		// the same way (uid + profile + phone).
		user = &models.User{
			UID:        uuid.NewString(),
			TelegramID: telegramID,
			Phone:      contact.PhoneNumber,
		}
		if msg.From != nil {
			user.Username = msg.From.Username
			user.FirstName = msg.From.FirstName
			user.LastName = msg.From.LastName
		}
		if err := h.store.SaveUser(ctx, user); err != nil {
			log.Printf("save user %d: %v", telegramID, err)
			h.sendText(ctx, b, msg.Chat.ID, "Не удалось сохранить данные. Попробуйте позже.")
			return
		}
		log.Printf("new user registered via contact: uid=%s telegram_id=%d",
			user.UID, user.TelegramID)

		h.sendMenu(ctx, b, msg.Chat.ID,
			fmt.Sprintf("Спасибо! Номер сохранён.\nUID: %s\n\nВыберите раздел:", user.UID))
		return
	}

	if err := h.store.UpdatePhone(ctx, user.UID, contact.PhoneNumber); err != nil {
		log.Printf("update phone for %s: %v", user.UID, err)
		h.sendText(ctx, b, msg.Chat.ID, "Не удалось сохранить номер. Попробуйте позже.")
		return
	}

	h.sendMenu(ctx, b, msg.Chat.ID, "Спасибо! Номер сохранён.\n\nВыберите раздел:")
}
