package storage

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"taro_bot/internal/models"
)

// openTestStorage connects to a real PostgreSQL database and prepares a
// clean state. The test is skipped unless TEST_DATABASE_URL is set, e.g.:
//
//	TEST_DATABASE_URL=postgres://taro:pass@127.0.0.1:5432/taro_bot_test?sslmode=disable go test ./internal/storage
//
// The database must exist; the schema and the tarot deck are created by
// Open.
func openTestStorage(t *testing.T) *Storage {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL tests")
	}

	s, err := Open(url)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	// TRUNCATE ... CASCADE wipes every table referencing users, leaving
	// the seeded tarot deck intact.
	if _, err := s.db.Exec(`TRUNCATE TABLE users CASCADE`); err != nil {
		t.Fatalf("clean tables: %v", err)
	}
	return s
}

func TestSeedTarotCards(t *testing.T) {
	ctx := context.Background()
	s := openTestStorage(t)

	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM tarot_cards`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 78 {
		t.Errorf("expected 78 cards, got %d", count)
	}

	// Spot checks on the seeded deck.
	card, err := s.GetTarotCard(ctx, 1)
	if err != nil {
		t.Fatalf("get card 1: %v", err)
	}
	if card.Name != "Шут" || card.ArcanaType != "старший" {
		t.Errorf("card 1 = %+v, want Шут/старший", card)
	}

	card, err = s.GetTarotCard(ctx, 22)
	if err != nil {
		t.Fatalf("get card 22: %v", err)
	}
	if card.Name != "Мир" {
		t.Errorf("card 22 = %q, want Мир", card.Name)
	}

	card, err = s.GetTarotCard(ctx, 78)
	if err != nil {
		t.Fatalf("get card 78: %v", err)
	}
	if card.Name != "Король Пентаклей" || card.ArcanaType != "младший" {
		t.Errorf("card 78 = %+v, want Король Пентаклей/младший", card)
	}

	if _, err := s.GetTarotCard(ctx, 99); !errors.Is(err, ErrNotFound) {
		t.Errorf("card 99: got %v, want ErrNotFound", err)
	}
}

func TestSeedTarotCardsIdempotent(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL tests")
	}

	// Open, close, and re-open: migrations and seeding must not duplicate
	// the deck.
	s1, err := Open(url)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	if _, err := s1.db.Exec(`TRUNCATE TABLE users CASCADE`); err != nil {
		t.Fatalf("clean tables: %v", err)
	}
	s1.Close()

	s2, err := Open(url)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer s2.Close()

	var count int
	if err := s2.db.QueryRow(`SELECT COUNT(*) FROM tarot_cards`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 78 {
		t.Errorf("expected 78 cards after reopen, got %d", count)
	}
}

func TestUserLifecycle(t *testing.T) {
	ctx := context.Background()
	s := openTestStorage(t)

	const (
		uid        = "test-uid-1"
		telegramID = int64(100200300)
	)
	u := &models.User{
		UID: uid, TelegramID: telegramID,
		Username: "taro_tester", FirstName: "Тест", LastName: "Бот",
	}
	if err := s.SaveUser(ctx, u); err != nil {
		t.Fatalf("save user: %v", err)
	}

	got, err := s.GetUserByTelegramID(ctx, telegramID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if got.UID != uid || got.Username != "taro_tester" {
		t.Errorf("user = %+v, want uid %s", got, uid)
	}
	if got.CreatedAt == "" {
		t.Errorf("created_at should be filled by the database default")
	}

	if err := s.UpdatePhone(ctx, uid, "+79990001122"); err != nil {
		t.Fatalf("update phone: %v", err)
	}
	got, err = s.GetUserByTelegramID(ctx, telegramID)
	if err != nil {
		t.Fatalf("get user after phone update: %v", err)
	}
	if got.Phone != "+79990001122" {
		t.Errorf("phone = %q, want +79990001122", got.Phone)
	}

	if _, err := s.GetUserByTelegramID(ctx, 999999999); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown telegram id: got %v, want ErrNotFound", err)
	}
	if err := s.UpdatePhone(ctx, "no-such-uid", "123"); !errors.Is(err, ErrNotFound) {
		t.Errorf("update phone for unknown uid: got %v, want ErrNotFound", err)
	}
}

func TestTarotRequestLifecycle(t *testing.T) {
	ctx := context.Background()
	s := openTestStorage(t)

	const uid = "test-uid-request"
	if err := s.SaveUser(ctx, &models.User{UID: uid, TelegramID: 777000111}); err != nil {
		t.Fatalf("save user: %v", err)
	}

	// Saving with a wrong number of cards is rejected.
	if _, err := s.SaveTarotRequest(ctx, uid, "вопрос", []int{1, 2}); err == nil {
		t.Errorf("expected error for 2 cards")
	}

	// A fresh request is stored with empty clarifier and answers.
	id, err := s.SaveTarotRequest(ctx, uid, "Что меня ждёт?", []int{5, 12, 33})
	if err != nil {
		t.Fatalf("save request: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}

	req, err := s.GetLatestTarotRequest(ctx, uid)
	if err != nil {
		t.Fatalf("get latest request: %v", err)
	}
	if req.ID != id || req.Request != "Что меня ждёт?" || len(req.Cards) != 3 {
		t.Errorf("request = %+v", req)
	}
	if req.ClarifierCard != 0 || req.AnswerInitial != "" || req.AnswerClarified != "" {
		t.Errorf("fresh request must have empty clarifier and answers, got %+v", req)
	}

	// The initial AI answer is stored on the same row.
	if err := s.UpdateTarotRequestAnswer(ctx, id, "прогноз"); err != nil {
		t.Fatalf("save answer: %v", err)
	}
	req, err = s.GetLatestTarotRequest(ctx, uid)
	if err != nil {
		t.Fatalf("get latest after answer: %v", err)
	}
	if req.AnswerInitial != "прогноз" {
		t.Errorf("AnswerInitial = %q, want прогноз", req.AnswerInitial)
	}

	// The clarifier card and its answer are stored on the same row.
	if err := s.UpdateTarotRequestClarifier(ctx, id, 42); err != nil {
		t.Fatalf("save clarifier: %v", err)
	}
	req, err = s.GetLatestTarotRequest(ctx, uid)
	if err != nil {
		t.Fatalf("get latest after clarifier: %v", err)
	}
	if req.ClarifierCard != 42 {
		t.Errorf("ClarifierCard = %d, want 42", req.ClarifierCard)
	}
	if err := s.UpdateTarotRequestClarifierAnswer(ctx, id, "итог"); err != nil {
		t.Fatalf("save clarifier answer: %v", err)
	}
	req, err = s.GetLatestTarotRequest(ctx, uid)
	if err != nil {
		t.Fatalf("get latest after clarifier answer: %v", err)
	}
	if req.AnswerClarified != "итог" {
		t.Errorf("AnswerClarified = %q, want итог", req.AnswerClarified)
	}

	// The most recent request wins.
	if _, err := s.SaveTarotRequest(ctx, uid, "второй запрос", []int{1, 2, 3}); err != nil {
		t.Fatalf("save second request: %v", err)
	}
	req, err = s.GetLatestTarotRequest(ctx, uid)
	if err != nil {
		t.Fatalf("get latest: %v", err)
	}
	if req.Request != "второй запрос" || req.ClarifierCard != 0 {
		t.Errorf("latest = %+v, want the new request without clarifier", req)
	}

	// Updates against unknown rows report ErrNotFound.
	if err := s.UpdateTarotRequestAnswer(ctx, 999999, "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("update answer for unknown id: got %v, want ErrNotFound", err)
	}
	if err := s.UpdateTarotRequestClarifier(ctx, 999999, 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("update clarifier for unknown id: got %v, want ErrNotFound", err)
	}
	if _, err := s.GetLatestTarotRequest(ctx, "no-such-user"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown uid: got %v, want ErrNotFound", err)
	}
}

func TestTarotRequestTTL(t *testing.T) {
	ctx := context.Background()
	s := openTestStorage(t)

	const uid = "test-uid-ttl"
	if err := s.SaveUser(ctx, &models.User{UID: uid, TelegramID: 777000333}); err != nil {
		t.Fatalf("save user: %v", err)
	}

	// A fresh request survives cleanup and is still returned.
	if _, err := s.SaveTarotRequest(ctx, uid, "свежий", []int{1, 2, 3}); err != nil {
		t.Fatalf("save fresh request: %v", err)
	}
	if err := s.CleanupTarotRequests(ctx); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if _, err := s.GetLatestTarotRequest(ctx, uid); err != nil {
		t.Fatalf("fresh request should be returned: %v", err)
	}

	// An expired request is not returned (created_at is TEXT
	// "YYYY-MM-DD HH24:MI:SS", compared lexicographically).
	old := time.Now().Add(-3 * 24 * time.Hour).UTC().Format("2006-01-02 15:04:05")
	if _, err := s.db.Exec(`UPDATE tarot_requests SET created_at = $1 WHERE uid = $2`, old, uid); err != nil {
		t.Fatalf("age request: %v", err)
	}
	if _, err := s.GetLatestTarotRequest(ctx, uid); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired request: got %v, want ErrNotFound", err)
	}

	// CleanupTarotRequests removes the expired row, keeping fresh ones.
	if _, err := s.SaveTarotRequest(ctx, uid, "второй", []int{4, 5, 6}); err != nil {
		t.Fatalf("save second request: %v", err)
	}
	if err := s.CleanupTarotRequests(ctx); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	var left int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM tarot_requests WHERE uid = $1`, uid).Scan(&left); err != nil {
		t.Fatalf("count: %v", err)
	}
	if left != 1 {
		t.Errorf("expected 1 request left, got %d", left)
	}
}

func TestNumerologyHistory(t *testing.T) {
	ctx := context.Background()
	s := openTestStorage(t)

	const uid = "test-uid-num"
	if err := s.SaveUser(ctx, &models.User{UID: uid, TelegramID: 777000222}); err != nil {
		t.Fatalf("save user: %v", err)
	}

	if err := s.SaveNumerologyRequest(ctx, uid, "15.03.1990", "14:30", "Москва", "натальная карта"); err != nil {
		t.Fatalf("save numerology request: %v", err)
	}

	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM numerology_requests WHERE uid = $1`, uid).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("rows = %d, want 1", n)
	}
}
