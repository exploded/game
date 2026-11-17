package handler

import (
	"fmt"
	"strings"

	"github.com/exploded/game/internal/db"
)

// shareResult is the text of the game home page's "Share my result" button
// (data-share in app.js), which players post to social media: their place
// and their return, and never what they hold, so it can't read as a tip
// about a stock. Rob saw no compliance issue with it (17 November 2026). It
// says the money is play money. It is "" for a player who isn't ranked yet.
func shareResult(game db.Game, s standing, total change, over bool) string {
	if s.Unranked {
		return ""
	}
	move := "level"
	if p := strings.TrimLeft(total.Pct(), "+-"); p != "" && p != "0.00%" {
		if total.Amount > 0 {
			move = "up " + p
		} else {
			move = "down " + p
		}
	}
	place := ""
	if s.Of > 1 {
		place = fmt.Sprintf(" %s of %d", s.Place(), s.Of)
	}
	money := fmtDollars(game.StartingBalance) + " of play money"
	switch {
	case over && place == "":
		return fmt.Sprintf("I finished The Investor Project's %s share game %s, with %s.", game.Name, move, money)
	case over:
		return fmt.Sprintf("I finished%s in The Investor Project's %s share game, %s with %s.", place, game.Name, move, money)
	case place == "":
		return fmt.Sprintf("I'm %s so far in The Investor Project's %s share game, with %s. Can you beat me?", move, game.Name, money)
	}
	return fmt.Sprintf("I'm%s in The Investor Project's %s share game, %s with %s. Can you beat me?", place, game.Name, move, money)
}
