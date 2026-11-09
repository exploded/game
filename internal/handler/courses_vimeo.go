package handler

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/vimeo"
)

// Filling a course from a Vimeo folder: the admin pastes the folder's link
// on /admin/courses/{id}, sees its videos, and imports them. Each video
// becomes a lesson (title, length, player link, thumbnail), in lesson
// order. A video already in the course only gets its thumbnail refreshed, so
// importing again adds new videos and picks up thumbnails set on Vimeo since.
// A video with no thumbnail on Vimeo gets none here, and the course pages
// show the course's picture instead.

// vimeoFolderView is a Vimeo folder as the admin course page shows it.
type vimeoFolderView struct {
	Link    string
	Folders []vimeo.Folder
	Videos  []vimeoVideoView
}

type vimeoVideoView struct {
	vimeo.Video
	Warning string // why it won't play on the site, or ""
}

// vimeoPlayable says why a video won't play embedded on the site, or "".
func vimeoPlayable(v vimeo.Video) string {
	switch {
	case v.EmbedOK == "private":
		return "Embedding is off for this video."
	case v.View == "nobody" || v.View == "contacts" || v.View == "users" || v.View == "password":
		return "Private (" + v.View + "): it won't play until its privacy is Unlisted or Hide from Vimeo."
	case v.EmbedOK == "whitelist":
		return "Embeds only on listed domains: check investorproject.au is one."
	}
	return ""
}

// vimeoFolder reads the folder a pasted link names.
func (h *Handler) vimeoFolder(r *http.Request, link string) (vimeoFolderView, error) {
	user, id, ok := vimeo.ParseFolderURL(link)
	if !ok {
		return vimeoFolderView{}, errors.New("That isn't a Vimeo folder link. Open the folder on vimeo.com and copy the address bar: it looks like https://vimeo.com/user/8765158/folder/26617132. Share links (vimeo.com/share/folder/…) don't work.")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	c, err := h.vimeo.Folder(ctx, user, id)
	if vimeo.NotFound(err) {
		return vimeoFolderView{}, errors.New("Vimeo says there's no such folder, or the token can't see it.")
	}
	if err != nil {
		slog.Error("vimeo folder", "folder", id, "error", err)
		return vimeoFolderView{}, errors.New("Couldn't read the folder from Vimeo. Try again in a minute.")
	}
	v := vimeoFolderView{Link: link, Folders: c.Folders}
	for _, video := range c.Videos {
		v.Videos = append(v.Videos, vimeoVideoView{video, vimeoPlayable(video)})
	}
	return v, nil
}

// AdminCourseVimeo adds a Vimeo folder's videos to the course as lessons.
func (h *Handler) AdminCourseVimeo(w http.ResponseWriter, r *http.Request) {
	c, ok := h.adminCourse(w, r)
	if !ok {
		return
	}
	back := "/admin/courses/" + strconv.FormatInt(c.ID, 10)
	if h.vimeo == nil {
		setFlashCookie(w, "Set VIMEO_TOKEN to fill courses from Vimeo.", "error")
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	link := r.FormValue("folder")
	folder, err := h.vimeoFolder(r, link)
	if err != nil {
		setFlashCookie(w, err.Error(), "error")
		http.Redirect(w, r, back+"?vimeo="+url.QueryEscape(link), http.StatusSeeOther)
		return
	}
	if err := h.q.SetCourseVimeoFolder(r.Context(), db.SetCourseVimeoFolderParams{VimeoFolder: folder.Link, ID: c.ID}); err != nil {
		slog.Warn("save course vimeo folder", "course", c.ID, "error", err)
	}
	added, existing, unplayable, noThumb := 0, 0, 0, 0
	for _, v := range folder.Videos {
		embed, ok := embedURL(v.Embed)
		if !ok {
			unplayable++
			continue
		}
		id, err := h.q.GetCourseLessonByVideo(r.Context(), db.GetCourseLessonByVideoParams{CourseID: c.ID, VideoUrl: v.Embed})
		switch {
		case err == nil:
			existing++
		case errors.Is(err, sql.ErrNoRows):
			title := v.Title()
			if len([]rune(title)) > 120 {
				title = string([]rune(title)[:120])
			}
			id, err = h.q.CreateLesson(r.Context(), db.CreateLessonParams{
				CourseID: c.ID, Title: title, VideoUrl: v.Embed, EmbedUrl: embed, Minutes: v.Minutes(),
			})
			if err != nil {
				h.serverError(w, "create lesson", err)
				return
			}
			added++
		default:
			h.serverError(w, "find lesson", err)
			return
		}
		if !h.copyLessonThumb(r, id, v.Thumb) {
			noThumb++
		}
	}
	msg := "Added " + plural(added, "lesson") + "."
	if existing > 0 {
		msg += " Updated the thumbnails of " + plural(existing, "lesson") + " already in the course."
	}
	if unplayable > 0 {
		msg += " Skipped " + plural(unplayable, "video") + " with no player link."
	}
	if noThumb > 0 {
		msg += " " + plural(noThumb, "lesson") + " show the course picture: Vimeo has no thumbnail for them."
	}
	setFlashCookie(w, msg, "success")
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// copyLessonThumb copies a video's thumbnail into lesson_thumbs, or removes
// the lesson's thumbnail when Vimeo has none (link ""). It says whether the
// lesson has a thumbnail now; a failed download keeps any it had.
func (h *Handler) copyLessonThumb(r *http.Request, lessonID int64, link string) bool {
	if link == "" {
		if err := h.q.DeleteLessonThumb(r.Context(), lessonID); err != nil {
			slog.Warn("delete lesson thumbnail", "lesson", lessonID, "error", err)
		}
		return false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	ct, data, err := h.vimeo.Thumbnail(ctx, link)
	cancel()
	if err == nil {
		err = h.q.SaveLessonThumb(r.Context(), db.SaveLessonThumbParams{LessonID: lessonID, ContentType: ct, Data: data})
	}
	if err != nil {
		slog.Warn("vimeo thumbnail", "lesson", lessonID, "error", err)
		return false
	}
	return true
}

// plural is "1 lesson" or "3 lessons".
func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}
