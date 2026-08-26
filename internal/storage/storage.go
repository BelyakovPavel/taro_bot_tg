// Package storage provides PostgreSQL persistence for bot users, tarot
// requests and AI request history.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL driver for database/sql

	"taro_bot/internal/models"
)

// ErrNotFound is returned when a requested row is not present in the
// database.
var ErrNotFound = errors.New("not found")

// spreadTTL is how long a stored tarot request lives before it expires.
const spreadTTL = 48 * time.Hour

// Storage wraps the PostgreSQL connection.
type Storage struct {
	db *sql.DB
}

// Open opens the PostgreSQL database at url (a pgx connection string such
// as postgres://user:pass@host:5432/db?sslmode=disable) and ensures the
// schema is up to date: migrations run and the tarot deck is seeded.
func Open(url string) (*Storage, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// PostgreSQL handles concurrency natively; use a small connection pool.
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	s := &Storage{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if err := s.seedTarotCards(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("seed tarot cards: %w", err)
	}
	return s, nil
}

// Close closes the underlying database connection.
func (s *Storage) Close() error {
	return s.db.Close()
}

// migrate creates the schema if needed and upgrades databases created by
// older versions to the current layout.
func (s *Storage) migrate() error {
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	return s.migrateLegacyTarotTables()
}

// migrateLegacyTarotTables upgrades databases from the previous design,
// where a tarot reading was split across three tables: user_spreads
// (query + cards + clarifier flag), tarot_clarifiers (drawn clarifier
// cards) and tarot_requests (request + cards only). The current design
// keeps everything in tarot_requests, so the legacy tables are dropped
// and the new columns are added (no-op on a fresh database).
func (s *Storage) migrateLegacyTarotTables() error {
	stmts := []string{
		`DROP TABLE IF EXISTS user_spreads`,
		`DROP TABLE IF EXISTS tarot_clarifiers`,
		`ALTER TABLE tarot_requests ADD COLUMN IF NOT EXISTS clarifier_card INTEGER`,
		`ALTER TABLE tarot_requests ADD COLUMN IF NOT EXISTS answer_initial TEXT`,
		`ALTER TABLE tarot_requests ADD COLUMN IF NOT EXISTS answer_clarified TEXT`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("legacy migration %q: %w", stmt, err)
		}
	}
	return nil
}

// seedTarotCards fills the tarot_cards table with the 78 cards of the
// deck. Idempotent: existing rows are left untouched.
func (s *Storage) seedTarotCards(ctx context.Context) error {
	const insert = `INSERT INTO tarot_cards (id, name, arcana_type, keywords, description)
	                VALUES ($1, $2, $3, $4, $5)
	                ON CONFLICT (id) DO NOTHING`
	for _, c := range models.TarotCards {
		if _, err := s.db.ExecContext(ctx, insert, c.ID, c.Name, c.ArcanaType, c.Keywords, c.Description); err != nil {
			return fmt.Errorf("insert tarot card %d: %w", c.ID, err)
		}
	}
	return nil
}

// GetTarotCard returns the card with the given ID (1..78),
// or ErrNotFound if there is none.
func (s *Storage) GetTarotCard(ctx context.Context, id int) (*models.TarotCard, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, arcana_type, keywords, description
		 FROM tarot_cards
		 WHERE id = $1`, id)

	c := &models.TarotCard{}
	err := row.Scan(&c.ID, &c.Name, &c.ArcanaType, &c.Keywords, &c.Description)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan tarot card: %w", err)
	}
	return c, nil
}

// SaveUser inserts a new user record.
func (s *Storage) SaveUser(ctx context.Context, u *models.User) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users (uid, telegram_id, username, first_name, last_name, phone)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		u.UID, u.TelegramID, u.Username, u.FirstName, u.LastName, u.Phone,
	)
	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

// GetUserByTelegramID returns the user with the given Telegram ID,
// or ErrNotFound if there is none.
func (s *Storage) GetUserByTelegramID(ctx context.Context, telegramID int64) (*models.User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT uid, telegram_id, username, first_name, last_name, phone, created_at
		 FROM users
		 WHERE telegram_id = $1`, telegramID)

	u := &models.User{}
	err := row.Scan(&u.UID, &u.TelegramID, &u.Username, &u.FirstName, &u.LastName, &u.Phone, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan user: %w", err)
	}
	return u, nil
}

// UpdatePhone sets the phone number of the user with the given uid.
func (s *Storage) UpdatePhone(ctx context.Context, uid, phone string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET phone = $1 WHERE uid = $2`, phone, uid)
	if err != nil {
		return fmt.Errorf("update phone: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SaveTarotRequest stores a tarot reading: the user's request and the
// three drawn cards. The clarifier card and both AI answers start empty
// and are filled in by the follow-up updates. It returns the row id.
func (s *Storage) SaveTarotRequest(ctx context.Context, uid, request string, cards []int) (int64, error) {
	if len(cards) != 3 {
		return 0, fmt.Errorf("expected 3 cards, got %d", len(cards))
	}

	var id int64
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO tarot_requests (uid, request, card1, card2, card3)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id`,
		uid, request, cards[0], cards[1], cards[2],
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert tarot request: %w", err)
	}
	return id, nil
}

// GetLatestTarotRequest returns the user's most recent request if it is
// still within the TTL. An expired request is simply not returned (the
// hourly cleanup deletes the row); ErrNotFound is reported otherwise.
func (s *Storage) GetLatestTarotRequest(ctx context.Context, uid string) (*models.TarotRequest, error) {
	cutoff := time.Now().Add(-spreadTTL).UTC().Format("2006-01-02 15:04:05")

	var (
		id              int64
		request         string
		c1, c2, c3      int64
		clarifierCard   int
		answerInitial   string
		answerClarified string
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, request, card1, card2, card3,
		        COALESCE(clarifier_card, 0),
		        COALESCE(answer_initial, ''),
		        COALESCE(answer_clarified, '')
		 FROM tarot_requests
		 WHERE uid = $1 AND created_at >= $2
		 ORDER BY id DESC
		 LIMIT 1`, uid, cutoff).
		Scan(&id, &request, &c1, &c2, &c3, &clarifierCard, &answerInitial, &answerClarified)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan tarot request: %w", err)
	}

	return &models.TarotRequest{
		ID:              id,
		UID:             uid,
		Request:         request,
		Cards:           []int{int(c1), int(c2), int(c3)},
		ClarifierCard:   clarifierCard,
		AnswerInitial:   answerInitial,
		AnswerClarified: answerClarified,
	}, nil
}

// UpdateTarotRequestAnswer stores the AI prediction for the three main
// cards on the request with the given id.
func (s *Storage) UpdateTarotRequestAnswer(ctx context.Context, id int64, answer string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE tarot_requests SET answer_initial = $1 WHERE id = $2`, answer, id)
	if err != nil {
		return fmt.Errorf("update tarot answer: %w", err)
	}
	return rowsAffected(res, "tarot answer")
}

// UpdateTarotRequestClarifier stores the clarifier card drawn for the
// request with the given id. Only one clarifier per request is allowed.
func (s *Storage) UpdateTarotRequestClarifier(ctx context.Context, id int64, cardID int) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE tarot_requests SET clarifier_card = $1 WHERE id = $2`, cardID, id)
	if err != nil {
		return fmt.Errorf("update tarot clarifier: %w", err)
	}
	return rowsAffected(res, "tarot clarifier")
}

// UpdateTarotRequestClarifierAnswer stores the AI explanation that
// covers the main cards plus the clarifier card.
func (s *Storage) UpdateTarotRequestClarifierAnswer(ctx context.Context, id int64, answer string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE tarot_requests SET answer_clarified = $1 WHERE id = $2`, answer, id)
	if err != nil {
		return fmt.Errorf("update tarot clarifier answer: %w", err)
	}
	return rowsAffected(res, "tarot clarifier answer")
}

// CleanupTarotRequests deletes all tarot requests older than the TTL.
// The created_at column is TEXT in "YYYY-MM-DD HH24:MI:SS" (UTC), which
// compares lexicographically like a timestamp, so a formatted cutoff
// works as a plain string comparison.
func (s *Storage) CleanupTarotRequests(ctx context.Context) error {
	cutoff := time.Now().Add(-spreadTTL).UTC().Format("2006-01-02 15:04:05")
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM tarot_requests WHERE created_at < $1`, cutoff)
	if err != nil {
		return fmt.Errorf("cleanup tarot requests: %w", err)
	}
	return nil
}

// rowsAffected maps an UPDATE that matched no rows to ErrNotFound.
func rowsAffected(res sql.Result, what string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected (%s): %w", what, err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SaveNumerologyRequest stores a numerology reading: the user's birth
// data (date, time, place) together with the AI-generated natal chart.
func (s *Storage) SaveNumerologyRequest(ctx context.Context, uid, birthDate, birthTime, birthPlace, result string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO numerology_requests (uid, birth_date, birth_time, birth_place, result)
		 VALUES ($1, $2, $3, $4, $5)`,
		uid, birthDate, birthTime, birthPlace, result,
	)
	if err != nil {
		return fmt.Errorf("insert numerology request: %w", err)
	}
	return nil
}
