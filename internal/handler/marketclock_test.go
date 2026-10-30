package handler

import (
	"testing"
	"time"

	"github.com/exploded/game/internal/market"
)

// setMarketNow sets the market clock and shows times in Sydney, for one test.
func setMarketNow(t *testing.T, at time.Time) {
	t.Helper()
	syd, err := time.LoadLocation("Australia/Sydney")
	if err != nil || market.NewYork == nil {
		t.Skip("no timezone database")
	}
	oldNow, oldLoc := marketNow, displayLoc
	marketNow, displayLoc = func() time.Time { return at }, syd
	t.Cleanup(func() { marketNow, displayLoc = oldNow, oldLoc })
}

// nyTime is a New York time, "2006-01-02 15:04".
func nyTime(t *testing.T, s string) time.Time {
	t.Helper()
	if market.NewYork == nil {
		t.Skip("no timezone database")
	}
	v, err := time.ParseInLocation("2006-01-02 15:04", s, market.NewYork)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestMarketOpens(t *testing.T) {
	tests := []struct{ at, want string }{
		// Saturday: the US clocks go back on 1 Nov, so Monday's open is 1:30 am Tuesday in Sydney.
		{"2026-10-31 08:00", "at 1:30 am AEDT on Tue 3 Nov"},
		{"2026-11-02 04:00", "in 5 hr 30 min, at 1:30 am AEDT tomorrow"},
		{"2026-11-02 09:10", "in 20 min, at 1:30 am AEDT"},
		{"2026-11-02 10:00", ""}, // the timetable says open, but the quotes don't
	}
	for _, tt := range tests {
		setMarketNow(t, nyTime(t, tt.at))
		if got := marketOpens(marketNow()); got != tt.want {
			t.Errorf("at %s NY: opens %q, want %q", tt.at, got, tt.want)
		}
	}
}

func TestMarketCloses(t *testing.T) {
	tests := []struct{ at, want string }{
		{"2026-11-02 10:00", "Closes in 6 hr"},
		{"2026-11-02 13:54", "Closes in 2 hr 6 min"},
		{"2026-11-27 12:15", "Closes early, in 45 min"},
		{"2026-10-31 12:00", ""}, // Saturday: the quotes say open, the timetable doesn't
	}
	for _, tt := range tests {
		setMarketNow(t, nyTime(t, tt.at))
		if got := marketCloses(marketNow()); got != tt.want {
			t.Errorf("at %s NY: %q, want %q", tt.at, got, tt.want)
		}
	}
}
