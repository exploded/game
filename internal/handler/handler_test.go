package handler

import "testing"

func TestFmtCents(t *testing.T) {
	for _, tc := range []struct {
		in   int64
		want string
	}{
		{0, "$0.00"},
		{5, "$0.05"},
		{123456789, "$1,234,567.89"},
		{-2000, "-$20.00"},
		{-50, "-$0.50"},
		{-100000005, "-$1,000,000.05"},
	} {
		if got := fmtCents(tc.in); got != tc.want {
			t.Errorf("fmtCents(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestAdminTab(t *testing.T) {
	for path, want := range map[string]string{
		"/admin":                "home",
		"/admin/":               "home",
		"/admin/games":          "games",
		"/admin/games/3":        "games",
		"/admin/learn/topics":   "learn",
		"/administrator":        "",
		"/games/1":              "",
		"/learn/questions/1234": "",
	} {
		if got := adminTab(path); got != want {
			t.Errorf("adminTab(%q) = %q, want %q", path, got, want)
		}
	}
}
