package market

import (
	"testing"

	"github.com/exploded/game/internal/db"
)

func TestTradeFees(t *testing.T) {
	aud := db.Game{BaseCurrency: "AUD", TradeFee: 500, FxFee: 50}
	for _, c := range []struct {
		name     string
		game     db.Game
		currency string
		value    int64
		want     int64
	}{
		{"same currency: flat fee only", aud, "AUD", 1000000, 500},
		{"other currency: 0.5% added", aud, "USD", 1000000, 500 + 5000},
		{"rounds to the nearest cent", aud, "USD", 1099, 500 + 5}, // 5.495c
		{"rounds half up", aud, "USD", 1100, 500 + 6},             // 5.5c
		{"no currency fee", db.Game{BaseCurrency: "AUD"}, "USD", 1000000, 0},
	} {
		if got := TradeFees(c.game, c.currency, c.value); got != c.want {
			t.Errorf("%s: TradeFees = %d, want %d", c.name, got, c.want)
		}
	}
}
