package handlers

import (
	"context"

	"github.com/go-telegram/bot"
	tgmodels "github.com/go-telegram/bot/models"
)

// privacySummary is the short privacy notice shown on /privacy. The full
// policy lives in PRIVACY.md at the repository root.
const privacySummary = `🔒 Конфиденциальность

Бот обрабатывает персональные данные в объёме, необходимом для работы:
• профиль Telegram (имя, username, id) — для регистрации;
• ваш запрос — для расклада Таро;
• дата, время и место рождения — для нумерологии;
• номер телефона — только если вы сами поделились им кнопкой ниже.

Данные передаются сервису ИИ (DeepSeek) исключительно для генерации ответов и не передаются иным третьим лицам. Запросы Таро хранятся 48 часов и затем удаляются автоматически. С полным текстом политики можно ознакомиться в файле PRIVACY.md репозитория проекта. По вопросам доступа, исправления или удаления ваших данных напишите администратору бота.`

// Privacy handles the "/privacy" command: it shows the short privacy
// notice.
func (h *Handler) Privacy(ctx context.Context, b *bot.Bot, update *tgmodels.Update) {
	if update.Message == nil {
		return
	}
	h.sendText(ctx, b, update.Message.Chat.ID, privacySummary)
}
