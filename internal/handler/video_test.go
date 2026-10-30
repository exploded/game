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
		"https://www.tiktok.com/@rob_goudie/video/7301234567890123456":    "https://www.tiktok.com/player/v1/7301234567890123456?rel=0&description=1",
		"https://www.tiktok.com/@rob_goudie/video/notanumber":             "",
		"https://www.facebook.com/consortiumpw/videos/123456789/":         "https://www.facebook.com/plugins/video.php?href=https%3A%2F%2Fwww.facebook.com%2Fwatch%2F%3Fv%3D123456789&show_text=false",
		"https://www.facebook.com/watch/?v=123456789":                     "https://www.facebook.com/plugins/video.php?href=https%3A%2F%2Fwww.facebook.com%2Fwatch%2F%3Fv%3D123456789&show_text=false",
		"https://www.instagram.com/reel/C1a2B3c4D5e/":                     "https://www.instagram.com/reel/C1a2B3c4D5e/embed",
		"https://www.instagram.com/p/C1a2B3c4D5e/?igsh=x":                 "https://www.instagram.com/p/C1a2B3c4D5e/embed",
		"https://www.instagram.com/consortiumprivatewealth/":              "",
	} {
		got, ok := embedURL(in)
		if got != want || ok != (want != "") {
			t.Errorf("embedURL(%q) = (%q, %v), want %q", in, got, ok, want)
		}
	}
}

func TestParseVideoVertical(t *testing.T) {
	for in, want := range map[string]bool{
		"https://youtube.com/shorts/dQw4w9WgXcQ":                       true,
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ":                  false,
		"https://www.tiktok.com/@rob_goudie/video/7301234567890123456": true,
		"https://www.instagram.com/reel/C1a2B3c4D5e/":                  true,
	} {
		v, ok := parseVideo(in)
		if !ok || v.Vertical != want {
			t.Errorf("parseVideo(%q) = %+v, %v; want vertical %v", in, v, ok, want)
		}
	}
}

func TestLocalPath(t *testing.T) {
	for in, want := range map[string]string{
		"/learn/live/3":         "/learn/live/3",
		"/learn?q=super":        "/learn?q=super",
		"":                      "/x",
		"//evil.example/":       "/x",
		`/\evil.example`:        "/x",
		"https://evil.example/": "/x",
		"learn":                 "/x",
	} {
		if got := localPath(in, "/x"); got != want {
			t.Errorf("localPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseTimestamp(t *testing.T) {
	for in, want := range map[string]int64{"90": 90, "1:30": 90, "1:02:03": 3723, "": 0, "a:b": 0, "-5": 0} {
		if got := parseTimestamp(in); got != want {
			t.Errorf("parseTimestamp(%q) = %d, want %d", in, got, want)
		}
	}
}
