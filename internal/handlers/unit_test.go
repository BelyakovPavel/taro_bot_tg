package handlers

import (
	"testing"

	"taro_bot/internal/models"
)

func TestParseBirthDate(t *testing.T) {
	valid := []struct{ in, want string }{
		{"15.03.1990", "15.03.1990"},
		{"15.3.1990", "15.03.1990"},
		{"15/03/1990", "15.03.1990"},
		{"15/3/1990", "15.03.1990"},
		{"1990-03-15", "15.03.1990"},
		{"1990-3-15", "15.03.1990"},
		{"  01.01.2000  ", "01.01.2000"},
	}
	for _, tc := range valid {
		got, err := parseBirthDate(tc.in)
		if err != nil {
			t.Errorf("parseBirthDate(%q): unexpected error %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseBirthDate(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	invalid := []string{"", "abc", "32.13.1990", "15.03.90", "15.03.1990-extra"}
	for _, in := range invalid {
		if _, err := parseBirthDate(in); err == nil {
			t.Errorf("parseBirthDate(%q): expected error", in)
		}
	}

	// Out of range: too early or in the future.
	if _, err := parseBirthDate("01.01.1899"); err == nil {
		t.Errorf("parseBirthDate(01.01.1899): expected error")
	}
	if _, err := parseBirthDate("01.01.2999"); err == nil {
		t.Errorf("parseBirthDate(01.01.2999): expected error")
	}
}

func TestParseBirthTime(t *testing.T) {
	valid := []struct{ in, want string }{
		{"14:30", "14:30"},
		{"14.30", "14:30"},
		{"14:30:45", "14:30"},
		{"не знаю", "не указано"},
		{"НЕ ПОМНЮ", "не указано"},
		{"неизвестно", "не указано"},
		{"unknown", "не указано"},
		{"-", "не указано"},
	}
	for _, tc := range valid {
		got, err := parseBirthTime(tc.in)
		if err != nil {
			t.Errorf("parseBirthTime(%q): unexpected error %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseBirthTime(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	for _, in := range []string{"", "abc", "25:99", "14:30:45:00"} {
		if _, err := parseBirthTime(in); err == nil {
			t.Errorf("parseBirthTime(%q): expected error", in)
		}
	}
}

func TestDrawCards(t *testing.T) {
	for _, count := range []int{1, 3, 5, 78} {
		cards := drawCards(count)
		if len(cards) != count {
			t.Fatalf("drawCards(%d) returned %d cards", count, len(cards))
		}
		seen := make(map[int]bool, count)
		for _, c := range cards {
			if c < 1 || c > 78 {
				t.Errorf("card %d out of range 1..78", c)
			}
			if seen[c] {
				t.Errorf("duplicate card %d in %v", c, cards)
			}
			seen[c] = true
		}
	}
}

func TestDisplayName(t *testing.T) {
	cases := []struct {
		name string
		user *models.User
		want string
	}{
		{"full name", &models.User{FirstName: "Иван", LastName: "Петров"}, "Иван Петров"},
		{"first only", &models.User{FirstName: "Иван"}, "Иван"},
		{"username fallback", &models.User{Username: "ivan"}, "@ivan"},
		{"empty", &models.User{}, "пользователь"},
	}
	for _, tc := range cases {
		if got := displayName(tc.user); got != tc.want {
			t.Errorf("%s: displayName = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestRandomDelay(t *testing.T) {
	// Zero bounds deliver immediately.
	if d := randomDelay(0, 0); d != 0 {
		t.Errorf("randomDelay(0,0) = %v, want 0", d)
	}
	// Result is always within [min, max].
	for i := 0; i < 100; i++ {
		if d := randomDelay(5, 10); d < 5 || d > 10 {
			t.Errorf("randomDelay(5,10) = %v, out of range", d)
		}
	}
	// Inverted bounds resolve to min without panicking.
	if d := randomDelay(10, 5); d != 10 {
		t.Errorf("randomDelay(10,5) = %v, want 10", d)
	}
}
