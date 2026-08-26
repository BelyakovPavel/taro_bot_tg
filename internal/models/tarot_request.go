package models

// TarotRequest is one stored tarot reading: the user's request, the
// three drawn cards, an optional clarifier card and the two AI answers.
//
// The row is created when the user sends the request (clarifier and
// answers are empty). The initial AI prediction fills AnswerInitial; if
// the user later draws a clarifier card, ClarifierCard and
// AnswerClarified are filled as well. Rows expire after a 48h TTL.
type TarotRequest struct {
	ID      int64
	UID     string
	Request string
	Cards   []int
	// ClarifierCard is the extra card drawn for this request, or 0 if
	// none has been drawn yet (the database column is NULL until then).
	ClarifierCard int
	// AnswerInitial is the AI prediction for the three main cards.
	AnswerInitial string
	// AnswerClarified is the AI explanation that also covers the
	// clarifier card; empty until the clarifier is drawn.
	AnswerClarified string
}
