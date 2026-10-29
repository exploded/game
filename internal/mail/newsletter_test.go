package mail

import "testing"

func TestNewNewsletter(t *testing.T) {
	n := NewNewsletter(" Hello ", "Line one\r\nline <two>\r\n\r\n\r\nSee https://example.com/a?b=1&c=2.")
	if n.Subject != "Hello" || len(n.Paragraphs) != 2 {
		t.Fatalf("got %q with %d paragraphs", n.Subject, len(n.Paragraphs))
	}
	if got := string(n.Paragraphs[0]); got != "Line one<br>line &lt;two&gt;" {
		t.Errorf("paragraph 1 = %q", got)
	}
	want := `See <a href="https://example.com/a?b=1&amp;c=2" style="color:#007994;" rel="nofollow noopener">https://example.com/a?b=1&amp;c=2</a>.`
	if got := string(n.Paragraphs[1]); got != want {
		t.Errorf("paragraph 2 = %q\nwant %q", got, want)
	}
}
