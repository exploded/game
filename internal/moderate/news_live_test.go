package moderate

import (
	"context"
	"os"
	"testing"

	"github.com/exploded/game/internal/market"
)

// TestNewsLive calls the real API with stories TickerTick gave for Amazon
// on 2 November 2026. Run it by hand with MODERATE_LIVE=1 and
// ANTHROPIC_API_KEY set; it costs well under a cent.
func TestNewsLive(t *testing.T) {
	if os.Getenv("MODERATE_LIVE") == "" {
		t.Skip("set MODERATE_LIVE=1 to call the Claude API")
	}
	stories := []struct {
		market.NewsStory
		keep bool
	}{
		{market.NewsStory{Site: "pymnts.com", Headline: "Amazon Positions Alexa for Shopping to Counter Third-Party Agents"}, true},
		{market.NewsStory{Site: "qz.com", Headline: "Amazon is setting up a loan facility of up to $80 billion to fund AI hardware"}, true},
		{market.NewsStory{Site: "yahoo.com", Headline: "Walmart's $120 Walmart+ and Sam's Club Plus bundle undercuts Amazon Prime's $139 fee"}, false},
		{market.NewsStory{Site: "theinventory.com", Headline: "Pawmate Duck Jerky Training Treats 10% Off on Amazon"}, false},
		{market.NewsStory{Site: "marketbeat.com", Headline: "Echostar (ECHO) Projected to Post Earnings on Thursday"}, false},
		{market.NewsStory{Site: "fool.com", Headline: "History Says Now Is the Perfect Time to Buy Amazon Stock"}, false},
		{market.NewsStory{Site: "yahoo.com", Headline: "Here's How Much a $1000 Investment in Amazon Made 10 Years Ago Would Be Worth Today"}, false},
		{market.NewsStory{Site: "thebrunswicknews.com", Headline: "A Whole Foods Market in the Miami suburb of Kendall, Fla."}, false},
		// Seen for other stocks: views on the shares, which the site mustn't
		// look like it's giving.
		{market.NewsStory{Site: "fool.com", Headline: "Amazon Raised Its 2026 Outlook. I'd Still Wait for a Lower Price to Buy the Stock."}, false},
		{market.NewsStory{Site: "yahoo.com", Headline: "Amazon.com (AMZN) Stock May Be 23% Undervalued As Q3 Beat Lifts Outlook"}, false},
	}
	offered := make([]market.NewsStory, len(stories))
	for i, s := range stories {
		offered[i] = s.NewsStory
	}
	keep, err := NewNews().Keep(context.Background(), "Amazon.com Inc", "AMZN", offered)
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range stories {
		t.Logf("keep=%v  %s", keep[i], s.Headline)
		if keep[i] != s.keep {
			t.Errorf("%q: keep = %v, want %v", s.Headline, keep[i], s.keep)
		}
	}
}
