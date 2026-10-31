package market

import "testing"

func TestMarketLabel(t *testing.T) {
	cases := []struct{ symbol, market, want string }{
		{"BHP", "sp500", "AU (US)"},
		{"CSLLY", "sp500", "AU (US)"},
		{"AAPL", "sp500", "SP500"},
		{"ASML", "sp500", "INTL (US)"},
		{"SHOP", "sp500", "INTL (US)"},
		{"IREN", "sp500", "AU (US)"},
		{"IBIT", "sp500", "US"},
		{"MSTR", "sp500", "US"},
		{"SPCX", "sp500", "US"},
		{"COIN", "sp500", "SP500"},
		// The old ASX row for BHP keeps its own market.
		{"BHP", "asx", "ASX"},
	}
	for _, c := range cases {
		if got := MarketLabel(c.symbol, c.market); got != c.want {
			t.Errorf("MarketLabel(%q, %q) = %q, want %q", c.symbol, c.market, got, c.want)
		}
	}
}
