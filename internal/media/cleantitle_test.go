package media

import "testing"

func TestCleanTitle(t *testing.T) {
	obj := string(rune(0xFFFC))    // TikTok's placeholder for a sticker
	zwsp := string(rune(0x200B))   // zero-width space
	zwj := string(rune(0x200D))    // joins emoji into one
	chart := string(rune(0x1F4C8)) // chart emoji
	family := string(rune(0x1F468)) + zwj + string(rune(0x1F469))
	for _, c := range []struct{ in, want string }{
		{obj + "Podcast recording", "Podcast recording"},
		{"  Q&A " + obj + "  live" + zwsp + " ", "Q&A live"},
		{"Markets " + chart + " today", "Markets " + chart + " today"},
		{"Family " + family, "Family " + family},
		{obj, ""},
	} {
		if got := cleanTitle(c.in); got != c.want {
			t.Errorf("cleanTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
