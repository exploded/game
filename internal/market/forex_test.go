package market

import "testing"

func TestParseRate(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int64
		ok   bool
	}{
		{"0.70268", 702680, true},
		{"0.6", 600000, true},
		{"0.1066", 0, false}, // the fake feed's rate: out of range
		{"6.4", 0, false},
		{"", 0, false},
		{"abc", 0, false},
	} {
		got, err := parseRate(tc.in)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("parseRate(%q) = %d, %v; want %d, ok %v", tc.in, got, err, tc.want, tc.ok)
		}
	}
}
