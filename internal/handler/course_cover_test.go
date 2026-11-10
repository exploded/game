package handler

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/exploded/game/internal/auth"
)

// pngOf makes a w × h PNG, see-through when alpha < 255.
func pngOf(t *testing.T, w, h int, alpha uint8) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 120, A: alpha})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// upload posts file as the form's "cover" field, with any extra fields.
func upload(srv http.Handler, user, path string, file []byte, fields map[string]string, cookie *http.Cookie) *httptest.ResponseRecorder {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	if file != nil {
		fw, _ := mw.CreateFormFile("cover", "cover.png")
		fw.Write(file)
	}
	mw.Close()
	req := httptest.NewRequest("POST", path, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-User", user)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func TestCourseCoverUpload(t *testing.T) {
	srv, d := courseServer(t)
	as(srv, "1", "POST", "/admin/courses", url.Values{"title": {"Estate Planning"}}, false)
	as(srv, "1", "POST", "/admin/courses/1/lessons", url.Values{"title": {"Why plan"}, "video_url": {"https://vimeo.com/76979871"}}, false)
	as(srv, "1", "POST", "/admin/courses/1/publish", url.Values{"publish": {"1"}}, false)

	if w := upload(srv, "2", "/admin/courses/1/cover", pngOf(t, 20, 10, 255), nil, nil); w.Code != http.StatusForbidden {
		t.Fatalf("a player uploading a cover: %d, want 403", w.Code)
	}

	// A big picture is scaled to fit 1280 pixels and stored as a JPEG.
	w := upload(srv, "1", "/admin/courses/1/cover", pngOf(t, 2000, 1000, 255), nil, nil)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin/courses/1#cover" {
		t.Fatalf("upload: %d to %q\n%s", w.Code, w.Header().Get("Location"), w.Body)
	}
	var ct string
	var data []byte
	var version int64
	d.QueryRow("SELECT content_type, data FROM course_covers WHERE course_id = 1").Scan(&ct, &data)
	d.QueryRow("SELECT cover_version FROM courses WHERE id = 1").Scan(&version)
	if ct != "image/jpeg" || version == 0 {
		t.Fatalf("stored %q, version %d; want image/jpeg and a version", ct, version)
	}
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil || cfg.Width != 1280 || cfg.Height != 640 {
		t.Errorf("stored cover is %dx%d (%v), want 1280x640", cfg.Width, cfg.Height, err)
	}

	// It replaces the drawing on the card, the course page, its lesson rows
	// and a visitor's locked lesson.
	cover := `<img class="course-art course-cover" src="/learn/course-cover/1?v=`
	for _, path := range []string{"/learn/courses", "/learn/courses/estate-planning", "/learn/courses/estate-planning/1", "/admin/courses/1"} {
		user := "2"
		if strings.HasPrefix(path, "/admin") {
			user = "1"
		}
		if page := as(srv, user, "GET", path, nil, false).Body.String(); !strings.Contains(page, cover) {
			t.Errorf("%s has no uploaded cover", path)
		}
	}
	if page := as(srv, "", "GET", "/learn/courses/estate-planning/1", nil, false).Body.String(); !strings.Contains(page, cover) {
		t.Error("a visitor's locked lesson has no uploaded cover")
	}

	w = as(srv, "", "GET", "/learn/course-cover/1?v=1", nil, false)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/jpeg" || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("serving the cover: %d %q %q", w.Code, w.Header().Get("Content-Type"), w.Header().Get("Cache-Control"))
	}

	// A small see-through picture stays a PNG, at its own size.
	upload(srv, "1", "/admin/courses/1/cover", pngOf(t, 320, 180, 128), nil, nil)
	var newVersion int64
	d.QueryRow("SELECT content_type, data FROM course_covers WHERE course_id = 1").Scan(&ct, &data)
	d.QueryRow("SELECT cover_version FROM courses WHERE id = 1").Scan(&newVersion)
	if cfg, _, _ := image.DecodeConfig(bytes.NewReader(data)); ct != "image/png" || cfg.Width != 320 {
		t.Errorf("see-through cover stored as %q, %d wide; want image/png, 320", ct, cfg.Width)
	}
	if newVersion == version {
		t.Error("a new upload kept the old version, so browsers keep the old picture")
	}

	// Something that isn't a picture, or no file, changes nothing.
	upload(srv, "1", "/admin/courses/1/cover", []byte("<svg></svg>"), nil, nil)
	upload(srv, "1", "/admin/courses/1/cover", nil, nil, nil)
	d.QueryRow("SELECT content_type FROM course_covers WHERE course_id = 1").Scan(&ct)
	if ct != "image/png" {
		t.Errorf("a bad upload replaced the cover: %q", ct)
	}

	// Removing it brings the drawing back.
	as(srv, "1", "POST", "/admin/courses/1/cover/delete", nil, false)
	d.QueryRow("SELECT cover_version FROM courses WHERE id = 1").Scan(&version)
	if version != 0 {
		t.Errorf("cover_version %d after removing, want 0", version)
	}
	if w := as(srv, "", "GET", "/learn/course-cover/1", nil, false); w.Code != http.StatusNotFound {
		t.Errorf("removed cover served: %d", w.Code)
	}
	if page := as(srv, "2", "GET", "/learn/courses/estate-planning", nil, false).Body.String(); strings.Contains(page, "course-cover") || !strings.Contains(page, "course-art-generic") {
		t.Error("the course page doesn't show its drawing after removing the cover")
	}

	// Deleting the course takes its cover with it.
	upload(srv, "1", "/admin/courses/1/cover", pngOf(t, 20, 10, 255), nil, nil)
	if _, err := d.Exec("DELETE FROM courses WHERE id = 1"); err != nil {
		t.Fatal(err)
	}
	var n int
	d.QueryRow("SELECT COUNT(*) FROM course_covers").Scan(&n)
	if n != 0 {
		t.Errorf("%d covers left after deleting the course", n)
	}
}

// A file upload carries its CSRF token in the multipart body, where
// ParseForm doesn't look.
func TestCSRFAcceptsMultipartToken(t *testing.T) {
	var token string
	csrf := auth.CSRFProtect([]byte("secret"))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token = auth.CSRFTokenFromContext(r.Context())
		if r.Method == http.MethodPost {
			if f, _, err := r.FormFile("cover"); err != nil {
				t.Errorf("handler can't read the file: %v", err)
			} else {
				f.Close()
			}
		}
	}))
	session := &http.Cookie{Name: "session", Value: "abc"}
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(session)
	csrf.ServeHTTP(httptest.NewRecorder(), req)

	if w := upload(csrf, "", "/", []byte("x"), map[string]string{"_csrf": token}, session); w.Code != http.StatusOK {
		t.Errorf("multipart with the right token: %d %s", w.Code, w.Body)
	}
	if w := upload(csrf, "", "/", []byte("x"), map[string]string{"_csrf": "wrong"}, session); w.Code != http.StatusForbidden {
		t.Errorf("multipart with a wrong token: %d, want 403", w.Code)
	}
	big := bytes.Repeat([]byte("x"), auth.MaxUploadBytes+1)
	if w := upload(csrf, "", "/", big, map[string]string{"_csrf": token}, session); w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("multipart over the limit: %d, want 413", w.Code)
	}
}
