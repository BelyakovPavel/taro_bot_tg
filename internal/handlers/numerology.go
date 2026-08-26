package handlers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	tgmodels "github.com/go-telegram/bot/models"

	"taro_bot/internal/models"
	"taro_bot/internal/payment"
	"taro_bot/internal/storage"
)

// numerologySystemPrompt instructs the AI how to write the natal chart
// from the birth data.
const numerologySystemPrompt = `Ты — опытный нумеролог и астролог-консультант. Пользователь указал дату, время и место рождения. На основе этих данных составь развёрнутую натальную карту и нумерологический разбор на русском языке. Структура ответа: короткое вступление; Число жизненного пути; Натальная карта (общий портрет личности); Характер и темперамент; Сильные стороны; Слабые стороны и зоны роста; Карьера и призвание; Любовь и отношения; Здоровье и энергия; Практический совет на ближайший год. Пиши тёплым, конкретным и практичным тоном, объёмом 25–35 предложений. Не упоминай, что ты искусственный интеллект, и не используй markdown-разметку.`

// Numerology handles the "Нумерология" button press: it checks the
// user's access to the feature and starts collecting the birth data
// (date, time, place) step by step.
func (h *Handler) Numerology(ctx context.Context, b *bot.Bot, update *tgmodels.Update) {
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

	if err := h.requireFeature(ctx, b, chatID, user, payment.FeatureNumerology); err != nil {
		return
	}

	h.setPendingState(chatID, pendingState{
		action:     pendingNumerologyDate,
		numerology: &numerologyInput{},
	})

	h.sendSubview(ctx, b, chatID,
		"🔢 Нумерология\n\nВведите дату рождения в формате ДД.ММ.ГГГГ, например 15.03.1990.")
}

// handleNumerologyDate processes the birth date the user typed and asks
// for the birth time next.
func (h *Handler) handleNumerologyDate(ctx context.Context, b *bot.Bot, update *tgmodels.Update, st pendingState) {
	chatID := update.Message.Chat.ID

	date, err := parseBirthDate(update.Message.Text)
	if err != nil {
		h.setPendingState(chatID, st) // keep waiting for a valid date
		h.sendText(ctx, b, chatID, "Не удалось распознать дату. Введите в формате ДД.ММ.ГГГГ, например 15.03.1990.")
		return
	}

	st.numerology.date = date
	st.action = pendingNumerologyTime
	h.setPendingState(chatID, st)

	h.sendText(ctx, b, chatID,
		"Отлично! Теперь введите время рождения в формате ЧЧ:ММ, например 14:30.\n"+
			"Если не знаете точное время, напишите «не знаю».")
}

// handleNumerologyTime processes the birth time the user typed and asks
// for the birth place next.
func (h *Handler) handleNumerologyTime(ctx context.Context, b *bot.Bot, update *tgmodels.Update, st pendingState) {
	chatID := update.Message.Chat.ID

	birthTime, err := parseBirthTime(update.Message.Text)
	if err != nil {
		h.setPendingState(chatID, st) // keep waiting for a valid time
		h.sendText(ctx, b, chatID, "Не удалось распознать время. Введите в формате ЧЧ:ММ, например 14:30, или напишите «не знаю».")
		return
	}

	st.numerology.time = birthTime
	st.action = pendingNumerologyPlace
	h.setPendingState(chatID, st)

	h.sendText(ctx, b, chatID,
		"И последний шаг — введите место рождения, например: Москва.")
}

// handleNumerologyPlace processes the birth place and starts the
// calculation once all three fields are collected.
func (h *Handler) handleNumerologyPlace(ctx context.Context, b *bot.Bot, update *tgmodels.Update, st pendingState) {
	chatID := update.Message.Chat.ID

	if update.Message.From == nil {
		return
	}

	place := strings.TrimSpace(update.Message.Text)
	if place == "" {
		h.setPendingState(chatID, st) // keep waiting for a valid place
		h.sendText(ctx, b, chatID, "Введите место рождения текстом, например: Москва.")
		return
	}
	if tooLong(place, maxNumerologyPlaceLen) {
		h.setPendingState(chatID, st) // keep waiting for a shorter place
		h.sendText(ctx, b, chatID,
			fmt.Sprintf("Слишком длинное название места — максимум %d символов. Попробуйте короче.", maxNumerologyPlaceLen))
		return
	}
	st.numerology.place = place

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

	if err := h.requireFeature(ctx, b, chatID, user, payment.FeatureNumerology); err != nil {
		return
	}

	h.startNumerology(ctx, b, chatID, user, st.numerology)
}

// startNumerology acknowledges the collected data and schedules the
// result: immediately in test mode, after a random 1-2 hours in working
// mode.
func (h *Handler) startNumerology(ctx context.Context, b *bot.Bot, chatID int64, user *models.User, input *numerologyInput) {
	if h.testMode {
		h.sendText(ctx, b, chatID, "🔢 Принято! Рассчитываю вашу натальную карту...")
	} else {
		h.sendText(ctx, b, chatID, fmt.Sprintf(
			"🔢 Принято!\n\nДата рождения: %s\nВремя рождения: %s\nМесто рождения: %s\n\n"+
				"Расчёт натальной карты займёт от 1 до 2 часов. Я напишу, когда всё будет готово.",
			input.date, input.time, input.place,
		))
	}

	var min, max time.Duration
	if h.testMode {
		min, max = 0, 0
	} else {
		min, max = h.delays.NumerologyMin, h.delays.NumerologyMax
	}

	h.schedule(ctx, min, max, func() {
		h.sendNumerologyResult(ctx, b, chatID, user, input)
	})
}

// sendNumerologyResult asks the AI for the natal chart and delivers it
// to the user in a separate message.
func (h *Handler) sendNumerologyResult(ctx context.Context, b *bot.Bot, chatID int64, user *models.User, input *numerologyInput) {
	userContent := fmt.Sprintf(
		"Дата рождения: %s\nВремя рождения: %s\nМесто рождения: %s",
		input.date, input.time, input.place,
	)

	result, err := h.ai.Generate(ctx, numerologySystemPrompt, userContent)
	if err != nil {
		log.Printf("ai numerology: %v", err)
		h.sendText(ctx, b, chatID, "Не удалось рассчитать натальную карту. Попробуйте позже ещё раз.")
		h.sendMenu(ctx, b, chatID, "Выберите раздел:")
		return
	}

	if err := h.store.SaveNumerologyRequest(ctx, user.UID, input.date, input.time, input.place, result); err != nil {
		log.Printf("save numerology request for %s: %v", user.UID, err)
	}

	h.sendText(ctx, b, chatID, "🔢 Ваша натальная карта:\n\n"+result)
	h.sendMenu(ctx, b, chatID, "Выберите раздел:")
}

// parseBirthDate recognizes common Russian and ISO date formats and
// normalizes the input to DD.MM.YYYY. The date must be between 1900 and
// today.
func parseBirthDate(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("empty birth date")
	}

	layouts := []string{
		"02.01.2006",
		"2.1.2006",
		"02/01/2006",
		"2/1/2006",
		"2006-01-02",
		"2006-1-2",
	}
	for _, layout := range layouts {
		t, err := time.Parse(layout, s)
		if err != nil {
			continue
		}
		if t.Year() < 1900 || t.After(time.Now()) {
			return "", errors.New("birth date out of range")
		}
		return t.Format("02.01.2006"), nil
	}
	return "", errors.New("invalid birth date")
}

// parseBirthTime recognizes HH:MM formats and the "unknown" keywords,
// normalizing the latter to "не указано".
func parseBirthTime(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("empty birth time")
	}

	switch strings.ToLower(s) {
	case "не знаю", "не помню", "неизвестно", "unknown", "-":
		return "не указано", nil
	}

	layouts := []string{"15:04", "15.04", "15:04:05"}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("15:04"), nil
		}
	}
	return "", errors.New("invalid birth time")
}
