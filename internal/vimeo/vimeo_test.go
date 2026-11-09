package vimeo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseFolderURL(t *testing.T) {
	cases := map[string]string{
		"https://vimeo.com/user/8765158/folder/26617132":      "8765158/26617132",
		"https://www.vimeo.com/user/8765158/folder/26617132/": "8765158/26617132",
		"https://vimeo.com/share/folder/2f489ff7-641d-4e05":   "",
		"https://vimeo.com/411280523":                         "",
		"https://example.com/user/1/folder/2":                 "",
		"not a link":                                          "",
	}
	for in, want := range cases {
		u, id, ok := ParseFolderURL(in)
		got := ""
		if ok {
			got = u + "/" + id
		}
		if got != want {
			t.Errorf("ParseFolderURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSortLessons(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 2, d, 0, 0, 0, 0, time.UTC) }
	numbered := []Video{{Name: "10. Wrap up", Created: day(1)}, {Name: "2. Shares", Created: day(2)}, {Name: "Lesson 1 - Intro", Created: day(3)}}
	SortLessons(numbered)
	if numbered[0].Name != "Lesson 1 - Intro" || numbered[2].Name != "10. Wrap up" {
		t.Errorf("numbered order = %v", numbered)
	}
	// One title without a number: upload order instead.
	mixed := []Video{{Name: "2. Shares", Created: day(3)}, {Name: "Welcome", Created: day(1)}, {Name: "1. Intro", Created: day(2)}}
	SortLessons(mixed)
	if mixed[0].Name != "Welcome" || mixed[2].Name != "2. Shares" {
		t.Errorf("mixed order = %v", mixed)
	}
}

func TestLessonTitle(t *testing.T) {
	zw := "\u200b"
	for in, want := range map[string]string{
		"Lesson 1: Estate Planning: Protecting Your Family": "Estate Planning: Protecting Your Family",
		"Lesson 3:  Understanding Tax Rules for Minors":     "Understanding Tax Rules for Minors",
		"Lesson 6Support Systems, Pitfalls, and Mindset":    "Support Systems, Pitfalls, and Mindset",
		"Lesson 5  Smart Strategies":                        "Smart Strategies",
		"Lesson 1: Investor Psychology: How Behaviour" + zw: "Investor Psychology: How Behaviour",
		"3. Diversification":                                "Diversification",
		"Part 2 \u2013 The Plan":                            "The Plan",
		"Welcome":                                           "Welcome",
		"2026 in review":                                    "2026 in review",
		"Lesson 4":                                          "Lesson 4",
	} {
		if got := LessonTitle(in); got != want {
			t.Errorf("LessonTitle(%q) = %q, want %q", in, got, want)
		}
	}
	// "Lesson 6Support" still counts as lesson 6 when sorting.
	if n := lessonNumber("Lesson 6Support Systems"); n != 6 {
		t.Errorf("lessonNumber = %d, want 6", n)
	}
}

func TestMinutesRoundsUp(t *testing.T) {
	for secs, want := range map[int]int64{0: 0, 1: 1, 60: 1, 61: 2, 599: 10} {
		if got := (Video{Seconds: secs}).Minutes(); got != want {
			t.Errorf("Minutes(%d s) = %d, want %d", secs, got, want)
		}
	}
}

// TestFolder reads a folder's items over two pages, with the token in the
// header and never in the URL.
func TestFolder(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "bearer tok" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		if strings.Contains(r.URL.RawQuery, "tok") {
			t.Errorf("token in URL: %s", r.URL)
		}
		switch {
		case r.URL.Path == "/me":
			json.NewEncoder(w).Encode(map[string]string{"name": "Consortium", "account": "basic"})
		case r.URL.Path == "/users/1/projects/2/items" && r.URL.Query().Get("page") == "":
			json.NewEncoder(w).Encode(map[string]any{
				"paging": map[string]string{"next": "/users/1/projects/2/items?page=2"},
				"data": []any{
					map[string]any{"type": "folder", "folder": map[string]any{"uri": "/users/1/projects/3", "name": "Sub"}},
					map[string]any{"type": "video", "video": map[string]any{
						"uri": "/videos/200", "name": "2. Shares", "duration": 125,
						"player_embed_url": "https://player.vimeo.com/video/200?h=abc",
						"pictures":         map[string]any{"sizes": []any{map[string]any{"width": 295, "link": "https://i.vimeocdn.com/s"}, map[string]any{"width": 640, "link": "https://i.vimeocdn.com/m"}, map[string]any{"width": 1280, "link": "https://i.vimeocdn.com/l"}}},
						"privacy":          map[string]string{"view": "unlisted", "embed": "public"},
					}},
				},
			})
		case r.URL.Path == "/users/1/projects/2/items":
			json.NewEncoder(w).Encode(map[string]any{"data": []any{
				map[string]any{"type": "video", "video": map[string]any{"uri": "/videos/100", "name": "1. Intro", "duration": 60}},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := New("tok").WithBase(srv.URL)
	a, err := c.Account(context.Background())
	if err != nil || a.Plan != "basic" || a.Paid() {
		t.Fatalf("Account = %+v, %v", a, err)
	}
	got, err := c.Folder(context.Background(), "1", "2")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Folders) != 1 || got.Folders[0].ID != "3" || got.Folders[0].User != "1" {
		t.Errorf("folders = %+v", got.Folders)
	}
	if len(got.Videos) != 2 || got.Videos[0].ID != "100" || got.Videos[1].ID != "200" {
		t.Fatalf("videos = %+v", got.Videos)
	}
	v := got.Videos[1]
	if v.Embed != "https://player.vimeo.com/video/200?h=abc" || v.Minutes() != 3 || v.Thumb != "https://i.vimeocdn.com/m" || v.View != "unlisted" {
		t.Errorf("video = %+v", v)
	}

	if _, err := c.Folder(context.Background(), "9", "9"); !NotFound(err) {
		t.Errorf("missing folder: err = %v, want not found", err)
	}
}

func TestNewWithoutToken(t *testing.T) {
	if New("") != nil {
		t.Error("New(\"\") should be nil")
	}
}
