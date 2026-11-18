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

// StopWorkingCalculator is "When can I stop working?": years of work by
// savings rate, plus an Australian mode with super's preservation age. Ported
// from James's prototype (C:\Projects\go\investor-tools). Hidden and
// browser-only, like MortgageCalculator (static/js/stop-working.js).
func (h *Handler) StopWorkingCalculator(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	h.render(w, r, "tools/stop-working", "", PageData{Title: "When can I stop working?"})
}

// MoneyLastedCalculator is "Would your money have lasted?": a retirement plan
// run through every start month of US market history since 1871, from
// Shiller's data (static/data/shiller.json). Ported from the same prototype.
// Hidden and browser-only (static/js/money-lasted.js).
func (h *Handler) MoneyLastedCalculator(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	h.render(w, r, "tools/money-lasted", "", PageData{Title: "Would your money have lasted?"})
}

// Tools is the calculators' home page: ours, then links to Moneysmart's for
// the sums we leave to it (super, pension and tax, whose rules change every
// year). Hidden like the calculators.
func (h *Handler) Tools(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	h.render(w, r, "tools/index", "", PageData{Title: "Calculators"})
}

// CompoundCalculator is our compound interest calculator, built to the
// assumptions Moneysmart states for its own: deposits at the end of each
// period, interest credited monthly or yearly, and a later-start comparison.
// Hidden and browser-only (static/js/compound.js).
func (h *Handler) CompoundCalculator(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	h.render(w, r, "tools/compound", "", PageData{Title: "Compound interest calculator"})
}

// FeesCalculator is our managed fund fee calculator, built to the assumptions
// Moneysmart states for its own: two funds' fees against no fees at all.
// Hidden and browser-only (static/js/fees.js).
func (h *Handler) FeesCalculator(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	h.render(w, r, "tools/fees", "", PageData{Title: "Managed fund fee calculator"})
}
