package market

import (
	"testing"
	"time"
)

// TestHolidays checks the rules against NYSE's published calendars.
func TestHolidays(t *testing.T) {
	want := map[int][]string{
		2026: {"2026-01-01", "2026-01-19", "2026-02-16", "2026-04-03", "2026-05-25", "2026-06-19", "2026-07-03", "2026-09-07", "2026-11-26", "2026-12-25"},
		2027: {"2027-01-01", "2027-01-18", "2027-02-15", "2027-03-26", "2027-05-31", "2027-06-18", "2027-07-05", "2027-09-06", "2027-11-25", "2027-12-24"},
		// New Year's Day 2028 is a Saturday: 31 Dec 2027 is a normal day.
		2028: {"2028-01-17", "2028-02-21", "2028-04-14", "2028-05-29", "2028-06-19", "2028-07-04", "2028-09-04", "2028-11-23", "2028-12-25"},
	}
	for y, days := range want {
		holiday := map[string]bool{}
		for _, d := range days {
			holiday[d] = true
		}
		for d := date(y, time.January, 1); d.Year() == y; d = d.AddDate(0, 0, 1) {
			if wd := d.Weekday(); wd == time.Saturday || wd == time.Sunday {
				continue
			}
			key := d.Format("2006-01-02")
			if got := isHoliday(d); got != holiday[key] {
				t.Errorf("%s (%s): holiday = %v, want %v", key, d.Weekday(), got, holiday[key])
			}
		}
	}
}

func TestEarlyCloses(t *testing.T) {
	early := map[string]bool{
		"2026-11-27": true, "2026-12-24": true, "2027-11-26": true, "2028-07-03": true, "2028-11-24": true,
		// The day before a holiday that isn't an early close.
		"2026-07-02": false, "2027-07-02": false, "2027-12-23": false, "2026-11-25": false,
	}
	for key, want := range early {
		d, _ := time.Parse("2006-01-02", key)
		if got := isEarlyClose(d); got != want {
			t.Errorf("%s: early close = %v, want %v", key, got, want)
		}
	}
}

func TestNextSession(t *testing.T) {
	if NewYork == nil {
		t.Skip("no timezone database")
	}
	ny := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, NewYork)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	tests := []struct {
		at, open, close string
	}{
		{"2026-10-30 10:00", "2026-10-30 09:30", "2026-10-30 16:00"}, // during a session
		{"2026-10-30 09:00", "2026-10-30 09:30", "2026-10-30 16:00"}, // before the open
		{"2026-10-30 16:00", "2026-11-02 09:30", "2026-11-02 16:00"}, // Friday's close: Monday
		{"2026-10-31 12:00", "2026-11-02 09:30", "2026-11-02 16:00"}, // Saturday
		{"2026-11-25 17:00", "2026-11-27 09:30", "2026-11-27 13:00"}, // over Thanksgiving to an early close
		{"2026-12-24 14:00", "2026-12-28 09:30", "2026-12-28 16:00"}, // after the early close, over Christmas
	}
	for _, tt := range tests {
		o, c := NextSession(ny(tt.at))
		if !o.Equal(ny(tt.open)) || !c.Equal(ny(tt.close)) {
			t.Errorf("at %s: session %s to %s, want %s to %s", tt.at,
				o.In(NewYork).Format("2006-01-02 15:04"), c.In(NewYork).Format("2006-01-02 15:04"), tt.open, tt.close)
		}
	}
	if _, c := NextSession(ny("2026-11-27 10:00")); !EarlyClose(c) {
		t.Error("27 Nov 2026 isn't an early close")
	}
	if _, c := NextSession(ny("2026-11-30 10:00")); EarlyClose(c) {
		t.Error("30 Nov 2026 is an early close")
	}
}
