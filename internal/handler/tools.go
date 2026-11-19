package handler

import "net/http"

// calculator is one of our calculators, as a card on /tools and the front of
// Learn (tool-card in partials/_tools.html).
type calculator struct {
	Slug  string // its page is /tools/{Slug}; also its card's art and colour
	Title string
	Blurb string
	Group string // the heading it sits under on /tools
}

// calculators lists every calculator in the order /tools shows them. The
// first three are the ones the front of Learn shows.
var calculators = []calculator{
	{"compound", "Compound interest calculator", "See how regular saving grows when your interest earns interest, and what starting later costs.", "Investing"},
	{"stop-working", "When can I stop working?", "Find out how many years of work your savings rate means, with super’s preservation age.", "Retirement and super"},
	{"mortgage", "Mortgage Freedom Calculator", "See how small extra repayments save interest and pay off your loan sooner.", "Buying a home"},
	{"fees", "Managed fund fee calculator", "Compare two funds’ fees and see how much they take from your balance over time.", "Investing"},
	{"money-lasted", "Would your money have lasted?", "Run a retirement plan through every start month of the US share market since 1871.", "Investing"},
	{"super", "Start Early: Extra Super Calculator", "See how a small extra weekly contribution could grow by 60 if you start at 20, 30 or 40.", "Retirement and super"},
	{"co-contribution", "Super Co-contribution Calculator", "See how much the government could add to your super, up to $500, when you put in after-tax money.", "Retirement and super"},
	{"spouse-contribution", "Spouse Contribution Calculator", "See the tax offset, up to $540, for putting money into your spouse’s super.", "Retirement and super"},
	{"fhss", "First Home Super Saver Calculator", "See how saving your first-home deposit through super compares with saving in a bank.", "Buying a home"},
}

// calculatorGroup is one heading on /tools and its calculators.
type calculatorGroup struct {
	Name  string
	Tools []calculator
}

// calculatorGroups sorts calculators under their headings, in the order each
// heading first appears.
func calculatorGroups() []calculatorGroup {
	var groups []calculatorGroup
	at := map[string]int{}
	for _, c := range calculators {
		i, ok := at[c.Group]
		if !ok {
			i = len(groups)
			at[c.Group] = i
			groups = append(groups, calculatorGroup{Name: c.Group})
		}
		groups[i].Tools = append(groups[i].Tools, c)
	}
	return groups
}

// MortgageCalculator is Rob's Mortgage Freedom Calculator, rebuilt from his
// spreadsheet (.local/calculators, August 2026). Since 18 November 2026 the
// calculators are a Learn tab (James's call); search engines are still asked
// to skip them. All the maths runs in the browser (static/js/mortgage.js). The figures
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

// Tools is the calculators' home page and Learn's Calculators tab: ours, by
// heading, then links to Moneysmart's for the sums we leave to it (super,
// pension and tax, whose rules change every year).
func (h *Handler) Tools(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	h.render(w, r, "tools/index", "", learnPage("tools", PageData{
		Title: "Calculators",
		Extra: map[string]any{"Groups": calculatorGroups(), "Count": len(calculators)},
	}))
}

// CompoundCalculator is our compound interest calculator, built to the
// assumptions Moneysmart states for its own: deposits at the end of each
// period, interest credited monthly or yearly, and a later-start comparison.
// Hidden and browser-only (static/js/compound.js).
func (h *Handler) CompoundCalculator(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	h.render(w, r, "tools/compound", "", PageData{Title: "Compound interest calculator"})
}

// fhssYear is one row of the FHSS calculator's contribution table.
type fhssYear struct {
	FY     string // "2026–27"
	Amount string // the sheet's example contribution, as typed
}

// FHSSCalculator is Rob's First Home Super Saver Calculator, rebuilt from his
// spreadsheet (.local/calculators/CPW_FHSS_Calculator_v2.xlsx, November 2026)
// with its 2026–27 caps and tax rates. Hidden from search engines and
// browser-only (static/js/fhss.js), like MortgageCalculator. The years and
// example amounts are the sheet's.
func (h *Handler) FHSSCalculator(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	years := []fhssYear{
		{"2026–27", "10,000"}, {"2027–28", "10,000"}, {"2028–29", "10,000"},
		{"2029–30", "15,000"}, {"2030–31", "5,000"}, {"2031–32", "0"},
	}
	h.render(w, r, "tools/fhss", "", PageData{
		Title: "First Home Super Saver Calculator",
		Extra: map[string]any{"Years": years},
	})
}

// CoContributionCalculator estimates the government co-contribution for
// 2026–27, and SpouseContributionCalculator the spouse contribution tax
// offset (Rob's ideas, November 2026). Both share static/js/contrib-calc.js,
// whose LAW holds the year's thresholds. Hidden from search engines and
// browser-only, like MortgageCalculator.
func (h *Handler) CoContributionCalculator(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	h.render(w, r, "tools/co-contribution", "", PageData{Title: "Super Co-contribution Calculator"})
}

// SpouseContributionCalculator: see CoContributionCalculator.
func (h *Handler) SpouseContributionCalculator(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	h.render(w, r, "tools/spouse-contribution", "", PageData{Title: "Spouse Contribution Calculator"})
}

// FeesCalculator is our managed fund fee calculator, built to the assumptions
// Moneysmart states for its own: two funds' fees against no fees at all.
// Hidden and browser-only (static/js/fees.js).
func (h *Handler) FeesCalculator(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	h.render(w, r, "tools/fees", "", PageData{Title: "Managed fund fee calculator"})
}
