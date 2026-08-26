package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-telegram/bot"
	tgmodels "github.com/go-telegram/bot/models"
	_ "github.com/jackc/pgx/v5/stdlib"

	"taro_bot/internal/ai"
	"taro_bot/internal/payment"
	"taro_bot/internal/storage"
)

// TestSmokeBotFlow drives a complete user journey through the real
// handler wiring: /start -> phone contact -> tarot spread -> clarifier
// card -> numerology. Storage runs against a real PostgreSQL database
// (TEST_DATABASE_URL), while the Telegram Bot API and the AI provider
// are mocked with httptest servers. All delivery delays are set to zero
// so the flow completes in milliseconds.
//
// Run with:
//
//	TEST_DATABASE_URL=postgres://taro:pass@127.0.0.1:5432/taro_bot_test?sslmode=disable go test ./internal/handlers -run TestSmokeBotFlow -v
func TestSmokeBotFlow(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping smoke test")
	}

	store, err := storage.Open(url)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	// A raw connection is used for test setup and direct assertions.
	raw, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	t.Cleanup(func() { raw.Close() })
	if _, err := raw.Exec(`TRUNCATE TABLE users CASCADE`); err != nil {
		t.Fatalf("clean tables: %v", err)
	}

	// --- AI provider mock -------------------------------------------------
	aiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		content := "ответ"
		for _, m := range req.Messages {
			switch {
			case strings.Contains(m.Content, "три выпавшие карты"):
				content = "прогноз-таро"
			case strings.Contains(m.Content, "пояснительная карта"):
				content = "объяснение-кларификатор"
			case strings.Contains(m.Content, "Дата рождения"):
				content = "натальная-карта-текст"
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"` + content + `"}}]}`))
	}))
	defer aiSrv.Close()

	aiClient := ai.NewClient("smoke-test-key")
	aiClient.SetBaseURL(aiSrv.URL)

	// --- Telegram Bot API mock -------------------------------------------
	const (
		chatID     = int64(424242)
		telegramID = int64(555666)
	)
	var (
		mu       sync.Mutex
		received []string
	)
	tgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		if method == "sendMessage" {
			mu.Lock()
			received = append(received, first(r.MultipartForm.Value["text"]))
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"date":1700000000,"chat":{"id":424242,"type":"private"}}}`))
	}))
	defer tgSrv.Close()

	h := New(store, aiClient, payment.New(nil), Delays{}, true)

	b, err := bot.New("12345:smoke-test-token",
		bot.WithServerURL(tgSrv.URL),
		bot.WithSkipGetMe(),
		bot.WithNotAsyncHandlers(),
		bot.WithDefaultHandler(h.Echo),
	)
	if err != nil {
		t.Fatalf("create bot: %v", err)
	}
	h.Register(b)

	// waitFor polls the recorded messages until a matching text arrives.
	waitFor := func(want string) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		var joined string
		for time.Now().Before(deadline) {
			mu.Lock()
			joined = strings.Join(received, "\n")
			mu.Unlock()
			if strings.Contains(joined, want) {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("timeout waiting for message containing %q; received:\n%s", want, joined)
	}

	// --- Drive the conversation ------------------------------------------
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	from := &tgmodels.User{ID: telegramID, Username: "smoketest", FirstName: "Смоук", LastName: "Тест"}
	chat := tgmodels.Chat{ID: chatID, Type: "private"}
	textUpd := func(id int, text string) *tgmodels.Update {
		return &tgmodels.Update{ID: int64(id), Message: &tgmodels.Message{ID: id, Chat: chat, From: from, Text: text}}
	}

	// 1. /start registers the user and asks for the phone number.
	b.ProcessUpdate(ctx, textUpd(1, "/start"))
	user, err := store.GetUserByTelegramID(ctx, telegramID)
	if err != nil {
		t.Fatalf("user not created on /start: %v", err)
	}
	if user.Phone != "" {
		t.Errorf("phone should be empty right after /start, got %q", user.Phone)
	}

	// 2. Sharing the contact saves the phone number.
	b.ProcessUpdate(ctx, &tgmodels.Update{ID: 2, Message: &tgmodels.Message{
		ID: 2, Chat: chat, From: from,
		Contact: &tgmodels.Contact{UserID: telegramID, PhoneNumber: "+79990001122"},
	}})
	user, err = store.GetUserByTelegramID(ctx, telegramID)
	if err != nil {
		t.Fatalf("get user after contact: %v", err)
	}
	if user.Phone != "+79990001122" {
		t.Errorf("phone = %q, want +79990001122", user.Phone)
	}

	// 3. Tarot: press the button, then send the query.
	b.ProcessUpdate(ctx, textUpd(3, BtnTarot))
	b.ProcessUpdate(ctx, textUpd(4, "Что меня ждёт в этом месяце?"))
	waitFor("🃏 Ваш расклад готов!")
	waitFor("🔮 Прогноз:\n\nпрогноз-таро")

	var spreads int
	if err := raw.QueryRow(`SELECT COUNT(*) FROM tarot_requests WHERE uid = $1`, user.UID).Scan(&spreads); err != nil {
		t.Fatalf("count tarot requests: %v", err)
	}
	if spreads != 1 {
		t.Errorf("expected 1 tarot request row, got %d", spreads)
	}
	// The AI answer was stored back on the same row.
	var answer string
	if err := raw.QueryRow(`SELECT COALESCE(answer_initial, '') FROM tarot_requests WHERE uid = $1`, user.UID).Scan(&answer); err != nil {
		t.Fatalf("read answer_initial: %v", err)
	}
	if !strings.Contains(answer, "прогноз-таро") {
		t.Errorf("answer_initial = %q, want it to contain the AI prediction", answer)
	}

	// 4. Clarifier: one extra card for the latest request.
	b.ProcessUpdate(ctx, textUpd(5, BtnClarifier))
	waitFor("✨ Пояснительная карта:")
	waitFor("🔮 Итоговое объяснение:\n\nобъяснение-кларификатор")

	var clarifierCard sql.NullInt64
	var clarifiedAnswer string
	if err := raw.QueryRow(`SELECT clarifier_card, COALESCE(answer_clarified, '') FROM tarot_requests WHERE uid = $1`, user.UID).Scan(&clarifierCard, &clarifiedAnswer); err != nil {
		t.Fatalf("read clarifier fields: %v", err)
	}
	if !clarifierCard.Valid || clarifierCard.Int64 < 1 || clarifierCard.Int64 > 78 {
		t.Errorf("clarifier_card = %v, want a card in 1..78", clarifierCard)
	}
	if !strings.Contains(clarifiedAnswer, "объяснение-кларификатор") {
		t.Errorf("answer_clarified = %q, want it to contain the clarifier explanation", clarifiedAnswer)
	}

	// 5. Numerology: date, time, place -> result (test mode delivers
	//    immediately).
	b.ProcessUpdate(ctx, textUpd(6, BtnNumerology))
	b.ProcessUpdate(ctx, textUpd(7, "15.03.1990"))
	b.ProcessUpdate(ctx, textUpd(8, "14:30"))
	b.ProcessUpdate(ctx, textUpd(9, "Москва"))
	waitFor("🔢 Ваша натальная карта:\n\nнатальная-карта-текст")

	var numerologies int
	if err := raw.QueryRow(`SELECT COUNT(*) FROM numerology_requests WHERE uid = $1`, user.UID).Scan(&numerologies); err != nil {
		t.Fatalf("count numerology: %v", err)
	}
	if numerologies != 1 {
		t.Errorf("expected 1 numerology row, got %d", numerologies)
	}

	mu.Lock()
	defer mu.Unlock()
	t.Logf("smoke flow OK; %d messages exchanged", len(received))
}

func snapshot(msgs []string) string {
	return strings.Join(msgs, "\n---\n")
}

func first(v []string) string {
	if len(v) == 0 {
		return ""
	}
	return v[0]
}
