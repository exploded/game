package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/exploded/game/internal/db"
)

// maxPickNote is how long Rob's line on a pick can be: one line on a phone.
const maxPickNote = 120

// AdminPicks lists Rob's Picks, the stocks page's default chip.
func (h *Handler) AdminPicks(w http.ResponseWriter, r *http.Request) {
	picks, err := h.q.ListStockPicks(r.Context())
	if err != nil {
		h.serverError(w, "list picks", err)
		return
	}
	h.render(w, r, "admin/picks", "", PageData{Title: "Rob's Picks", Items: picks})
}

// pickNote reads the note from the form, returning a message saying what's
// wrong or "".
func pickNote(r *http.Request) (note, msg string) {
	note = strings.Join(strings.Fields(r.FormValue("note")), " ")
	if utf8.RuneCountInString(note) > maxPickNote {
		msg = fmt.Sprintf("Keep the note to %d characters.", maxPickNote)
	}
	return
}

// AdminPickAdd adds a stock to the end of Rob's Picks, by its symbol. A
// stock that's already a pick gets the new note.
func (h *Handler) AdminPickAdd(w http.ResponseWriter, r *http.Request) {
	symbol := strings.ToUpper(strings.TrimSpace(r.FormValue("symbol")))
	note, msg := pickNote(r)
	var stock db.Stock
	if msg == "" {
		var err error
		// Every stock players can trade is in the sp500 market.
		stock, err = h.q.GetStockBySymbol(r.Context(), db.GetStockBySymbolParams{Symbol: symbol, Market: "sp500"})
		if err != nil {
			msg = fmt.Sprintf("There's no stock %q.", symbol)
		}
	}
	if msg != "" {
		setFlashCookie(w, msg, "error")
		http.Redirect(w, r, "/admin/picks", http.StatusSeeOther)
		return
	}
	if err := h.q.AddStockPick(r.Context(), db.AddStockPickParams{StockID: stock.ID, Note: note}); err != nil {
		h.serverError(w, "add pick", err)
		return
	}
	h.screener.clear()
	setFlashCookie(w, stock.Symbol+" is one of Rob's Picks.", "success")
	http.Redirect(w, r, "/admin/picks", http.StatusSeeOther)
}

// AdminPickUpdate changes a pick's note and place.
func (h *Handler) AdminPickUpdate(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("sid"), 10, 64)
	note, msg := pickNote(r)
	order, _ := strconv.ParseInt(r.FormValue("sort_order"), 10, 64)
	if msg != "" {
		setFlashCookie(w, msg, "error")
	} else if err := h.q.UpdateStockPick(r.Context(), db.UpdateStockPickParams{Note: note, SortOrder: order, StockID: id}); err != nil {
		h.serverError(w, "update pick", err)
		return
	} else {
		h.screener.clear()
		setFlashCookie(w, "Pick saved.", "success")
	}
	http.Redirect(w, r, "/admin/picks", http.StatusSeeOther)
}

// AdminPickDelete takes a stock out of Rob's Picks.
func (h *Handler) AdminPickDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("sid"), 10, 64)
	if err := h.q.DeleteStockPick(r.Context(), id); err != nil {
		h.serverError(w, "delete pick", err)
		return
	}
	h.screener.clear()
	setFlashCookie(w, "Pick removed.", "success")
	http.Redirect(w, r, "/admin/picks", http.StatusSeeOther)
}
