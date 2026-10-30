package market

import "time"

// The US market's timetable, used only to tell players when it next opens or
// closes. Whether it is open for trading comes from QuoteStatus, which also
// covers closures nobody can predict, such as a national day of mourning.

// NextSession returns the opening and closing times of the US market's regular
// session under way at t, or of the next one if none is. Both are zero if the
// New York timezone is missing.
func NextSession(t time.Time) (open, close time.Time) {
	if NewYork == nil {
		return
	}
	y, m, d := t.In(NewYork).Date()
	for i := range 14 {
		if o, c, ok := session(y, m, d+i); ok && t.Before(c) {
			return o, c
		}
	}
	return
}

// EarlyClose reports whether a session closes early, at 1:00 pm New York time.
func EarlyClose(close time.Time) bool {
	return NewYork != nil && close.In(NewYork).Hour() == 13
}

// session returns the regular session on a New York date: 9:30 am to 4:00 pm,
// or to 1:00 pm on an early close. ok is false on weekends and holidays.
// time.Date normalises the date, so d can run past the end of the month.
func session(y int, m time.Month, d int) (open, close time.Time, ok bool) {
	day := date(y, m, d)
	if wd := day.Weekday(); wd == time.Saturday || wd == time.Sunday || isHoliday(day) {
		return
	}
	closeHour := 16
	if isEarlyClose(day) {
		closeHour = 13
	}
	y, m, d = day.Date()
	return time.Date(y, m, d, 9, 30, 0, 0, NewYork), time.Date(y, m, d, closeHour, 0, 0, 0, NewYork), true
}

// isHoliday reports whether the NYSE is shut on a weekday, by its holiday
// rules: a holiday on a Saturday moves to the Friday before, and one on a
// Sunday to the Monday after, except that New Year's Day on a Saturday isn't
// moved back into the old year.
func isHoliday(day time.Time) bool {
	y := day.Year()
	holidays := []time.Time{
		nthWeekday(y, time.January, time.Monday, 3),    // Martin Luther King Jr. Day
		nthWeekday(y, time.February, time.Monday, 3),   // Washington's Birthday
		easter(y).AddDate(0, 0, -2),                    // Good Friday
		nthWeekday(y, time.May, time.Monday, -1),       // Memorial Day
		observed(date(y, time.June, 19)),               // Juneteenth
		observed(date(y, time.July, 4)),                // Independence Day
		nthWeekday(y, time.September, time.Monday, 1),  // Labor Day
		nthWeekday(y, time.November, time.Thursday, 4), // Thanksgiving
		observed(date(y, time.December, 25)),           // Christmas Day
	}
	if nyd := date(y, time.January, 1); nyd.Weekday() != time.Saturday {
		holidays = append(holidays, observed(nyd))
	}
	for _, h := range holidays {
		if day.Equal(h) {
			return true
		}
	}
	return false
}

// isEarlyClose reports whether a trading day closes at 1:00 pm: the day after
// Thanksgiving, and 3 July and Christmas Eve when they fall Monday to
// Thursday (on a Friday they're the observed holiday).
func isEarlyClose(day time.Time) bool {
	y, m, d := day.Date()
	wd := day.Weekday()
	switch {
	case m == time.July && d == 3, m == time.December && d == 24:
		return wd >= time.Monday && wd <= time.Thursday
	case m == time.November:
		return day.Equal(nthWeekday(y, time.November, time.Thursday, 4).AddDate(0, 0, 1))
	}
	return false
}

// date is a calendar date, at midnight UTC so that adding days is exact.
func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// observed moves a holiday on a weekend to the nearest weekday.
func observed(day time.Time) time.Time {
	switch day.Weekday() {
	case time.Saturday:
		return day.AddDate(0, 0, -1)
	case time.Sunday:
		return day.AddDate(0, 0, 1)
	}
	return day
}

// nthWeekday returns the nth given weekday of a month, or the last for n = -1.
func nthWeekday(y int, m time.Month, wd time.Weekday, n int) time.Time {
	if n < 0 {
		last := date(y, m+1, 0)
		return last.AddDate(0, 0, -((int(last.Weekday()) - int(wd) + 7) % 7))
	}
	first := date(y, m, 1)
	return first.AddDate(0, 0, (int(wd)-int(first.Weekday())+7)%7+7*(n-1))
}

// easter returns Easter Sunday (the anonymous Gregorian algorithm).
func easter(y int) time.Time {
	a, b, c := y%19, y/100, y%100
	d, e := b/4, b%4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i, k := c/4, c%4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	day := (h+l-7*m+114)%31 + 1
	return date(y, time.Month(month), day)
}
