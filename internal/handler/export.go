package handler

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
)

// csvSafe prevents CSV injection by prefixing dangerous leading characters with a single quote.
func csvSafe(s string) string {
	if len(s) > 0 {
		switch s[0] {
		case '=', '+', '-', '@', '\t', '\r':
			return "'" + s
		}
	}
	return s
}

// safeFilename strips characters that are unsafe in Content-Disposition filenames.
var unsafeFilenameChars = regexp.MustCompile(`[^a-zA-Z0-9._-]`)

func safeFilename(name string) string {
	s := strings.ReplaceAll(name, " ", "_")
	return unsafeFilenameChars.ReplaceAllString(s, "")
}

// ExportTransactionsCSV exports transaction history as CSV (Feature 23).
func (h *Handler) ExportTransactionsCSV(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	gameID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	game, err := h.q.GetGame(r.Context(), gameID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	participant, err := h.q.GetParticipantByGameAndUser(r.Context(), db.GetParticipantByGameAndUserParams{
		GameID: gameID, UserID: user.ID,
	})
	if err != nil {
		http.Error(w, "Not a participant", http.StatusForbidden)
		return
	}

	txns, _ := h.q.ListAllTransactionsForExport(r.Context(), participant.ID)

	filename := safeFilename(game.Name) + "-transactions.csv"
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))

	w.Write([]byte("Date,Type,Symbol,Name,Market,Quantity,Price,Total,Converted Total,Exchange Rate,Fee\n"))
	for _, t := range txns {
		w.Write([]byte(fmt.Sprintf("%s,%s,%s,%s,%s,%d,%.2f,%.2f,%.2f,%.6f,%.2f\n",
			csvSafe(t.CreatedAt),
			csvSafe(t.Type),
			csvSafe(t.Symbol),
			csvSafe(t.StockName),
			csvSafe(t.Market),
			t.Quantity,
			float64(t.Price)/100,
			float64(t.Total)/100,
			float64(t.ConvertedTotal)/100,
			float64(t.ExchangeRateUsed)/1000000,
			float64(t.Fee)/100,
		)))
	}
}

// ExportSnapshotsCSV exports portfolio snapshots as CSV (Feature 23).
func (h *Handler) ExportSnapshotsCSV(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	gameID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	game, err := h.q.GetGame(r.Context(), gameID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	participant, err := h.q.GetParticipantByGameAndUser(r.Context(), db.GetParticipantByGameAndUserParams{
		GameID: gameID, UserID: user.ID,
	})
	if err != nil {
		http.Error(w, "Not a participant", http.StatusForbidden)
		return
	}

	snapshots, _ := h.q.ListSnapshotsForExport(r.Context(), participant.ID)

	filename := safeFilename(game.Name) + "-portfolio-history.csv"
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))

	w.Write([]byte("Date,Cash Balance,Holdings Value,Total Value\n"))
	for _, s := range snapshots {
		w.Write([]byte(fmt.Sprintf("%s,%.2f,%.2f,%.2f\n",
			s.Date,
			float64(s.CashBalance)/100,
			float64(s.HoldingsValue)/100,
			float64(s.TotalValue)/100,
		)))
	}
}
