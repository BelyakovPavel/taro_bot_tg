package storage

// schema is the PostgreSQL schema, created with CREATE TABLE IF NOT
// EXISTS on startup. All created_at columns are TEXT holding UTC
// timestamps in "YYYY-MM-DD HH24:MI:SS" format, which compares
// lexicographically like a timestamp (used by the TTL cleanup).
const schema = `
	CREATE TABLE IF NOT EXISTS users (
		uid         TEXT    PRIMARY KEY,
		telegram_id BIGINT  NOT NULL UNIQUE,
		username    TEXT    NOT NULL DEFAULT '',
		first_name  TEXT    NOT NULL DEFAULT '',
		last_name   TEXT    NOT NULL DEFAULT '',
		phone       TEXT    NOT NULL DEFAULT '',
		created_at  TEXT    NOT NULL DEFAULT to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS')
	);

	CREATE TABLE IF NOT EXISTS tarot_cards (
		id          INTEGER PRIMARY KEY,
		name        TEXT NOT NULL,
		arcana_type TEXT NOT NULL,
		keywords    TEXT NOT NULL,
		description TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS tarot_requests (
		id               BIGSERIAL PRIMARY KEY,
		uid              TEXT    NOT NULL REFERENCES users(uid),
		request          TEXT    NOT NULL,
		card1            INTEGER NOT NULL,
		card2            INTEGER NOT NULL,
		card3            INTEGER NOT NULL,
		clarifier_card   INTEGER,
		answer_initial   TEXT,
		answer_clarified TEXT,
		created_at       TEXT    NOT NULL DEFAULT to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS')
	);

	CREATE TABLE IF NOT EXISTS numerology_requests (
		id          BIGSERIAL PRIMARY KEY,
		uid         TEXT    NOT NULL REFERENCES users(uid),
		birth_date  TEXT    NOT NULL,
		birth_time  TEXT    NOT NULL,
		birth_place TEXT    NOT NULL,
		result      TEXT    NOT NULL,
		created_at  TEXT    NOT NULL DEFAULT to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS')
	);
`
