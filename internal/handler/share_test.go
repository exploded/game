package handler

import (
	"testing"

	"github.com/exploded/game/internal/db"
)

func TestShareResult(t *testing.T) {
	g := db.Game{Name: "November 2026", StartingBalance: 100000000}
	up := change{Amount: 4210000, Base: 100000000}
	down := change{Amount: -2100000, Base: 100000000}
	for _, c := range []struct {
		s     standing
		total change
		over  bool
		want  string
	}{
		{standing{Rank: 3, Of: 120}, up, false, "I'm 3rd of 120 in The Investor Project's November 2026 share game, up 4.21% with $1,000,000 of play money. Can you beat me?"},
		{standing{Rank: 12, Of: 120}, down, true, "I finished 12th of 120 in The Investor Project's November 2026 share game, down 2.10% with $1,000,000 of play money."},
		{standing{Rank: 1, Of: 1}, change{Base: 100000000}, false, "I'm level so far in The Investor Project's November 2026 share game, with $1,000,000 of play money. Can you beat me?"},
		{standing{Rank: 1, Of: 1}, up, true, "I finished The Investor Project's November 2026 share game up 4.21%, with $1,000,000 of play money."},
		{standing{Unranked: true}, up, false, ""},
	} {
		if got := shareResult(g, c.s, c.total, c.over); got != c.want {
			t.Errorf("shareResult(%+v, %+v, %v)\n got %q\nwant %q", c.s, c.total, c.over, got, c.want)
		}
	}
}
