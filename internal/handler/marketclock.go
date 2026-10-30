package handler

import (
	"fmt"
	"math"
	"time"

	"github.com/exploded/game/internal/market"
)

// marketNow is the time, for the market clock. A variable so tests can set it.
var marketNow = time.Now

// nextMarketChange returns when the US market next opens (open false) or
// closes (open true), from its timetable. It is zero when the timetable
// disagrees with open, as when the quotes stop or the market shuts for a day
// the timetable doesn't know about: the wording then leaves the time out.
func nextMarketChange(t time.Time, open bool) time.Time {
	o, c := market.NextSession(t)
	if o.IsZero() || t.Before(o) == open {
		return time.Time{}
	}
	if open {
		return c
	}
	return o
}

// marketOpens finishes the sentence "It opens …" while the US market is
// closed: "in 3 hr 20 min, at 12:30 am AEDT tomorrow", or "at 12:30 am AEDT on
// Tue 3 Nov" when that's a day or more away. Empty if it isn't known.
func marketOpens(t time.Time) string {
	o := nextMarketChange(t, false)
	if o.IsZero() {
		return ""
	}
	at := "at " + localClock(o, t)
	if o.Sub(t) >= 24*time.Hour {
		return at
	}
	return "in " + shortDuration(o.Sub(t)) + ", " + at
}

// marketCloses says when the US market closes while it's open: "Closes in
// 2 hr 5 min", or "Closes early, in 45 min" on a 1:00 pm close. Empty if it
// isn't known.
func marketCloses(t time.Time) string {
	c := nextMarketChange(t, true)
	if c.IsZero() {
		return ""
	}
	if market.EarlyClose(c) {
		return "Closes early, in " + shortDuration(c.Sub(t))
	}
	return "Closes in " + shortDuration(c.Sub(t))
}

// shortDuration rounds up to the minute: "45 min", "3 hr", "3 hr 20 min".
func shortDuration(d time.Duration) string {
	mins := int(math.Ceil(d.Minutes()))
	h, m := mins/60, mins%60
	switch {
	case h == 0:
		return fmt.Sprintf("%d min", m)
	case m == 0:
		return fmt.Sprintf("%d hr", h)
	}
	return fmt.Sprintf("%d hr %d min", h, m)
}

// localClock gives t in the site's time zone, with the day when it isn't
// today: "7:00 am AEDT", "12:30 am AEDT tomorrow", "1:30 am AEDT on Tue 3 Nov".
func localClock(t, from time.Time) string {
	lt, lf := t.In(displayLoc), from.In(displayLoc)
	s := lt.Format("3:04 pm MST")
	ty, tm, td := lt.Date()
	fy, fm, fd := lf.Date()
	days := time.Date(ty, tm, td, 0, 0, 0, 0, time.UTC).Sub(time.Date(fy, fm, fd, 0, 0, 0, 0, time.UTC)) / (24 * time.Hour)
	switch days {
	case 0:
		return s
	case 1:
		return s + " tomorrow"
	}
	return s + " on " + lt.Format("Mon 2 Jan")
}
