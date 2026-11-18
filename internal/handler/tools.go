package handler

import "net/http"

// MortgageCalculator is Rob's Mortgage Freedom Calculator, rebuilt from his
// spreadsheet (.local/calculators, August 2026). It's a hidden mock-up for
// Rob to review: nothing links to it, and search engines are asked to skip
// it. All the maths runs in the browser (static/js/mortgage.js). The figures
// a visitor types are never sent to the server, so nothing about their loan
// is stored or logged.
func (h *Handler) MortgageCalculator(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	h.render(w, r, "tools/mortgage", "", PageData{Title: "Mortgage Freedom Calculator"})
}

// SuperCalculator is Rob's Start Early: Extra Super Calculator, rebuilt from
// his spreadsheet (.local/calculators, August 2026). Hidden and browser-only,
// like MortgageCalculator (static/js/super.js).
func (h *Handler) SuperCalculator(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	h.render(w, r, "tools/super", "", PageData{Title: "Start Early: Extra Super Calculator"})
}
