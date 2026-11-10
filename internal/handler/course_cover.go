package handler

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // registers WebP with image.Decode
)

// An admin can upload a picture for a course's cover. It's resized and
// stored in course_covers, and replaces the drawn art (courseArts) on the
// course card, the course page and lessons without their own thumbnail.
// courses.cover_version goes in the picture's URL, so a new upload shows
// straight away though the picture is cached for a year.

const (
	// coverMaxBytes is the largest file accepted. auth.MaxUploadBytes, the
	// whole form, must stay above it.
	coverMaxBytes = 10 << 20
	// coverMaxSide is the most pixels a stored cover has across or down. The
	// widest place it shows is the course page's 260px panel, so this is
	// plenty for sharp screens and keeps a cover around 100-200 KB.
	coverMaxSide = 1280
	// coverMaxPixels refuses images too big to decode safely.
	coverMaxPixels = 50_000_000
)

var errCoverFormat = errors.New("Upload a JPEG, PNG or WebP picture.")

// courseCoverImage checks an uploaded picture and returns it ready to store:
// scaled to fit coverMaxSide, and re-encoded (which also drops any camera
// details, such as where a photo was taken). A picture with transparency
// stays PNG; anything else becomes JPEG.
func courseCoverImage(data []byte) (contentType string, out []byte, err error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png" && format != "webp") {
		return "", nil, errCoverFormat
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width*cfg.Height > coverMaxPixels {
		return "", nil, fmt.Errorf("That picture is %d × %d pixels. Upload one under 50 megapixels.", cfg.Width, cfg.Height)
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", nil, errCoverFormat
	}

	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > coverMaxSide || h > coverMaxSide {
		if w >= h {
			w, h = coverMaxSide, max(1, h*coverMaxSide/w)
		} else {
			w, h = max(1, w*coverMaxSide/h), coverMaxSide
		}
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
		src = dst
	}

	var buf bytes.Buffer
	if o, ok := src.(interface{ Opaque() bool }); ok && !o.Opaque() {
		if err := png.Encode(&buf, src); err != nil {
			return "", nil, err
		}
		return "image/png", buf.Bytes(), nil
	}
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 85}); err != nil {
		return "", nil, err
	}
	return "image/jpeg", buf.Bytes(), nil
}

// courseCoverURL is where a course's uploaded cover is served.
func courseCoverURL(courseID, version int64) string {
	return fmt.Sprintf("/learn/course-cover/%d?v=%d", courseID, version)
}

// CourseCover serves a course's uploaded cover.
func (h *Handler) CourseCover(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("cid"), 10, 64)
	c, err := h.q.GetCourseCover(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.serverError(w, "get course cover", err)
		return
	}
	w.Header().Set("Content-Type", c.ContentType)
	if r.URL.Query().Get("v") != "" {
		// Each upload gets a new ?v=, so this address never changes.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
	w.Write(c.Data)
}

// AdminCourseCover stores an uploaded cover for a course.
func (h *Handler) AdminCourseCover(w http.ResponseWriter, r *http.Request) {
	c, ok := h.adminCourse(w, r)
	if !ok {
		return
	}
	back := "/admin/courses/" + strconv.FormatInt(c.ID, 10) + "#cover"
	fail := func(msg string) {
		setFlashCookie(w, msg, "error")
		http.Redirect(w, r, back, http.StatusSeeOther)
	}
	// CSRFProtect has normally parsed the form already, within its own limit.
	r.Body = http.MaxBytesReader(w, r.Body, auth.MaxUploadBytes)
	f, _, err := r.FormFile("cover")
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		fail("That picture is too big. Upload one under 10 MB.")
		return
	}
	if err != nil {
		fail("Choose a picture to upload.")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, coverMaxBytes+1))
	if err != nil {
		h.serverError(w, "read course cover", err)
		return
	}
	if len(data) > coverMaxBytes {
		fail("That picture is too big. Upload one under 10 MB.")
		return
	}
	ct, img, err := courseCoverImage(data)
	if err != nil {
		fail(err.Error())
		return
	}

	tx, err := h.rawDB.BeginTx(r.Context(), nil)
	if err != nil {
		h.serverError(w, "save course cover", err)
		return
	}
	defer tx.Rollback()
	qtx := h.q.WithTx(tx)
	if err := qtx.SaveCourseCover(r.Context(), db.SaveCourseCoverParams{CourseID: c.ID, ContentType: ct, Data: img}); err != nil {
		h.serverError(w, "save course cover", err)
		return
	}
	if err := qtx.SetCourseCoverVersion(r.Context(), db.SetCourseCoverVersionParams{CoverVersion: time.Now().UnixMilli(), ID: c.ID}); err != nil {
		h.serverError(w, "save course cover", err)
		return
	}
	if err := tx.Commit(); err != nil {
		h.serverError(w, "save course cover", err)
		return
	}
	setFlashCookie(w, "Cover picture uploaded.", "success")
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// AdminCourseCoverDelete removes a course's uploaded cover, so it shows its
// drawn art again.
func (h *Handler) AdminCourseCoverDelete(w http.ResponseWriter, r *http.Request) {
	c, ok := h.adminCourse(w, r)
	if !ok {
		return
	}
	tx, err := h.rawDB.BeginTx(r.Context(), nil)
	if err != nil {
		h.serverError(w, "remove course cover", err)
		return
	}
	defer tx.Rollback()
	qtx := h.q.WithTx(tx)
	if err := qtx.DeleteCourseCover(r.Context(), c.ID); err != nil {
		h.serverError(w, "remove course cover", err)
		return
	}
	if err := qtx.SetCourseCoverVersion(r.Context(), db.SetCourseCoverVersionParams{CoverVersion: 0, ID: c.ID}); err != nil {
		h.serverError(w, "remove course cover", err)
		return
	}
	if err := tx.Commit(); err != nil {
		h.serverError(w, "remove course cover", err)
		return
	}
	setFlashCookie(w, "Cover picture removed. The course shows its drawing again.", "success")
	http.Redirect(w, r, "/admin/courses/"+strconv.FormatInt(c.ID, 10)+"#cover", http.StatusSeeOther)
}
