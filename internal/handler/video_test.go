package handler

import "testing"

func TestEmbedURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=10s":               "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?rel=0",
		"https://youtu.be/dQw4w9WgXcQ":                                    "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?rel=0",
		"https://youtube.com/shorts/dQw4w9WgXcQ":                          "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?rel=0",
		" https://www.youtube.com/embed/dQw4w9WgXcQ ":                     "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?rel=0",
		"https://vimeo.com/76979871":                                      "https://player.vimeo.com/video/76979871",
		"https://vimeo.com/76979871/8272103f6e":                           "https://player.vimeo.com/video/76979871?h=8272103f6e",
		"https://player.vimeo.com/video/76979871?h=8272103f6e&badge=0":    "https://player.vimeo.com/video/76979871?h=8272103f6e",
		"https://iframe.mediadelivery.net/play/12345/abc-def":             "https://iframe.mediadelivery.net/embed/12345/abc-def",
		"https://iframe.mediadelivery.net/embed/12345/abc-def?autoplay=1": "https://iframe.mediadelivery.net/embed/12345/abc-def",
		"https://example.com/video.mp4":                                   "",
		"javascript:alert(1)":                                             "",
		"https://www.youtube.com/watch?v=bad\"id":                         "",
		"https://vimeo.com/channels/staffpicks":                           "",
		"https://youtu.be/":                                               "",
	} {
		got, ok := embedURL(in)
		if got != want || ok != (want != "") {
			t.Errorf("embedURL(%q) = (%q, %v), want %q", in, got, ok, want)
		}
	}
}
