package handler

import (
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/mail"
)

// Courses are pre-recorded video lessons. Anyone can browse them at
// /learn/courses and see each course's lessons; playing a lesson and ticking
// it off need an account. Admins build them as drafts on /admin/courses and
// publish when ready.

// coursePath is a course's page, or one of its lessons with lessonID > 0.
func coursePath(slug string, lessonID int64) string {
	p := "/learn/courses/" + slug
	if lessonID > 0 {
		p += "/" + strconv.FormatInt(lessonID, 10)
	}
	return p
}

// CoursesPage lists the published courses with the viewer's progress.
func (h *Handler) CoursesPage(w http.ResponseWriter, r *http.Request) {
	courses, err := h.q.ListPublishedCourses(r.Context(), viewerID(r))
	if err != nil {
		h.serverError(w, "list courses", err)
		return
	}
	h.render(w, r, "courses/list", "", learnPage(r, "courses", PageData{Title: "Courses", Items: courses}))
}

// OldCourseURL sends the courses' old addresses (/courses/...) to their
// place under /learn.
func (h *Handler) OldCourseURL(w http.ResponseWriter, r *http.Request) {
	target := "/learn" + r.URL.EscapedPath()
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, target, http.StatusMovedPermanently)
}

// course loads the {slug} course. Drafts are visible to admins only.
func (h *Handler) course(w http.ResponseWriter, r *http.Request) (db.Course, []db.ListLessonsRow, bool) {
	user := auth.UserFromContext(r.Context())
	c, err := h.q.GetCourseBySlug(r.Context(), r.PathValue("slug"))
	if errors.Is(err, sql.ErrNoRows) || (err == nil && c.Published == 0 && (user == nil || user.IsAdmin == 0)) {
		http.NotFound(w, r)
		return c, nil, false
	}
	if err != nil {
		h.serverError(w, "get course", err)
		return c, nil, false
	}
	lessons, err := h.q.ListLessons(r.Context(), db.ListLessonsParams{Viewer: viewerID(r), CourseID: c.ID})
	if err != nil {
		h.serverError(w, "list lessons", err)
		return c, nil, false
	}
	return c, lessons, true
}

// CoursePage shows a course: what it covers and its lessons, with a button
// to start or carry on from the first lesson not yet done.
func (h *Handler) CoursePage(w http.ResponseWriter, r *http.Request) {
	c, lessons, ok := h.course(w, r)
	if !ok {
		return
	}
	done, resume := 0, -1
	var minutes int64
	for i, l := range lessons {
		minutes += l.Minutes
		if l.Completed {
			done++
		} else if resume < 0 {
			resume = i
		}
	}
	extra := map[string]any{"Done": done, "Minutes": minutes, "Outline": courseOutline(c.Summary)}
	if resume >= 0 {
		extra["Resume"] = lessons[resume]
		extra["ResumeNumber"] = resume + 1
	}
	if len(lessons) > 0 {
		extra["Pct"] = done * 100 / len(lessons)
	}
	h.render(w, r, "courses/course", "", learnPage(r, "courses", PageData{
		Title: c.Title, Item: c, Items: lessons, Extra: extra,
	}))
}

// LessonPage plays one lesson, with links to the lessons either side. A
// visitor sees the lesson with a sign-in button where the player goes.
func (h *Handler) LessonPage(w http.ResponseWriter, r *http.Request) {
	c, lessons, ok := h.course(w, r)
	if !ok {
		return
	}
	i := lessonIndex(lessons, r.PathValue("lid"))
	if i < 0 {
		http.NotFound(w, r)
		return
	}
	extra := map[string]any{"Course": c, "Number": i + 1, "Count": len(lessons)}
	if i > 0 {
		extra["Prev"] = lessons[i-1]
	}
	if i+1 < len(lessons) {
		extra["Next"] = lessons[i+1]
	}
	h.render(w, r, "courses/lesson", "", learnPage(r, "courses", PageData{
		Title: lessons[i].Title, Item: lessons[i], Items: lessons, Extra: extra,
	}))
}

// LessonComplete marks a lesson done (or not done, with done=0) and moves on
// to the next lesson.
func (h *Handler) LessonComplete(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	c, lessons, ok := h.course(w, r)
	if !ok {
		return
	}
	i := lessonIndex(lessons, r.PathValue("lid"))
	if i < 0 {
		http.NotFound(w, r)
		return
	}
	if r.FormValue("done") == "0" {
		_ = h.q.UncompleteLesson(r.Context(), db.UncompleteLessonParams{UserID: user.ID, LessonID: lessons[i].ID})
		http.Redirect(w, r, coursePath(c.Slug, lessons[i].ID), http.StatusSeeOther)
		return
	}
	if err := h.q.CompleteLesson(r.Context(), db.CompleteLessonParams{UserID: user.ID, LessonID: lessons[i].ID}); err != nil {
		h.serverError(w, "complete lesson", err)
		return
	}
	if i+1 < len(lessons) {
		http.Redirect(w, r, coursePath(c.Slug, lessons[i+1].ID), http.StatusSeeOther)
		return
	}
	setFlashCookie(w, "You've finished "+c.Title+". Nice work.", "success")
	http.Redirect(w, r, coursePath(c.Slug, 0), http.StatusSeeOther)
}

func lessonIndex(lessons []db.ListLessonsRow, id string) int {
	for i, l := range lessons {
		if strconv.FormatInt(l.ID, 10) == id {
			return i
		}
	}
	return -1
}

// LessonThumb serves a lesson's thumbnail, copied in from Vimeo.
func (h *Handler) LessonThumb(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("lid"), 10, 64)
	t, err := h.q.GetLessonThumb(r.Context(), id)
	if err != nil {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		fmt.Fprint(w, placeholderThumb)
		return
	}
	w.Header().Set("Content-Type", t.ContentType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(t.Data)
}

// outline is a course summary split for the course page: the paragraphs
// before the "what you'll learn" list, the list, and any paragraphs after.
type outline struct {
	Intro []template.HTML
	Learn []string
	Outro []template.HTML
}

// courseOutline splits a summary. Lines starting with a ballot box (as in
// Anne and Rob's course sheet) are the list. The paragraph introducing it
// ("In this course, you'll learn about:") is dropped, since the page has its
// own heading.
func courseOutline(summary string) outline {
	var o outline
	var before, after []string
	for _, line := range strings.Split(strings.ReplaceAll(summary, "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(line)
		if item, ok := strings.CutPrefix(t, "☑"); ok {
			if item = strings.TrimSpace(strings.TrimPrefix(item, "️")); item != "" {
				o.Learn = append(o.Learn, item)
			}
			continue
		}
		if len(o.Learn) == 0 {
			before = append(before, line)
		} else {
			after = append(after, line)
		}
	}
	intro := strings.TrimSpace(strings.Join(before, "\n"))
	if len(o.Learn) > 0 && strings.HasSuffix(intro, ":") {
		if i := strings.LastIndex(intro, "\n\n"); i >= 0 {
			intro = strings.TrimSpace(intro[:i])
		} else {
			intro = ""
		}
	}
	o.Intro = mail.Paragraphs(intro)
	o.Outro = mail.Paragraphs(strings.Join(after, "\n"))
	return o
}

// courseArts are the cover illustrations an admin can pick for a course
// (course-art in partials/_learnart.html). "" is the generic one.
var courseArts = []struct{ Key, Name string }{
	{"", "Generic (play button)"},
	{"market", "Share market (candles)"},
	{"snowball", "Compounding (growing bars)"},
	{"warning", "Mistakes (warning sign)"},
	{"brain", "Psychology (head)"},
	{"scales", "Shares vs property (scales)"},
	{"nestegg", "Super (nest egg)"},
	{"home", "First home (house)"},
	{"loan", "Home loan (house and key)"},
	{"family", "Children (seedling)"},
	{"estate", "Estate planning (shield)"},
}

func validArt(key string) bool {
	for _, a := range courseArts {
		if a.Key == key {
			return true
		}
	}
	return false
}

// AdminCourses lists every course, drafts included, with a form to start one.
func (h *Handler) AdminCourses(w http.ResponseWriter, r *http.Request) {
	courses, err := h.q.ListAllCourses(r.Context())
	if err != nil {
		h.serverError(w, "list courses", err)
		return
	}
	h.render(w, r, "admin/courses", "", PageData{Title: "Courses", Items: courses})
}

// AdminCourseCreate starts a draft course and opens it for editing.
func (h *Handler) AdminCourseCreate(w http.ResponseWriter, r *http.Request) {
	title := strings.Join(strings.Fields(r.FormValue("title")), " ")
	if title == "" || utf8.RuneCountInString(title) > 120 {
		setFlashCookie(w, "Give the course a title of up to 120 characters.", "error")
		http.Redirect(w, r, "/admin/courses", http.StatusSeeOther)
		return
	}
	slug := slugify(title)
	for n := 2; ; n++ {
		if _, err := h.q.GetCourseBySlug(r.Context(), slug); errors.Is(err, sql.ErrNoRows) {
			break
		}
		slug = slugify(title) + "-" + strconv.Itoa(n)
	}
	id, err := h.q.CreateCourse(r.Context(), db.CreateCourseParams{Slug: slug, Title: title})
	if err != nil {
		h.serverError(w, "create course", err)
		return
	}
	http.Redirect(w, r, "/admin/courses/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

// adminCourse loads the {cid} course for the admin pages.
func (h *Handler) adminCourse(w http.ResponseWriter, r *http.Request) (db.Course, bool) {
	id, err := strconv.ParseInt(r.PathValue("cid"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return db.Course{}, false
	}
	c, err := h.q.GetCourse(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return c, false
	}
	return c, true
}

// AdminCourse edits a course and its lessons.
func (h *Handler) AdminCourse(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	c, ok := h.adminCourse(w, r)
	if !ok {
		return
	}
	lessons, err := h.q.ListLessons(r.Context(), db.ListLessonsParams{Viewer: user.ID, CourseID: c.ID})
	if err != nil {
		h.serverError(w, "list lessons", err)
		return
	}
	extra := map[string]any{"Arts": courseArts, "Vimeo": h.vimeo != nil}
	// The box opens on the folder the course was last filled from.
	extra["VimeoLink"] = c.VimeoFolder
	if link := strings.TrimSpace(r.URL.Query().Get("vimeo")); link != "" && h.vimeo != nil {
		extra["VimeoLink"] = link
		contents, err := h.vimeoFolder(r, link)
		if err != nil {
			extra["VimeoError"] = err.Error()
		} else {
			extra["VimeoFolder"] = contents
		}
	}
	h.render(w, r, "admin/course", "", PageData{Title: c.Title, Item: c, Items: lessons, Extra: extra})
}

// AdminCourseUpdate saves the course's title, summary and order.
func (h *Handler) AdminCourseUpdate(w http.ResponseWriter, r *http.Request) {
	c, ok := h.adminCourse(w, r)
	if !ok {
		return
	}
	title := strings.Join(strings.Fields(r.FormValue("title")), " ")
	if title == "" {
		title = c.Title
	}
	order, _ := strconv.ParseInt(r.FormValue("sort_order"), 10, 64)
	art := r.FormValue("art")
	if !validArt(art) {
		art = c.Art
	}
	if err := h.q.UpdateCourse(r.Context(), db.UpdateCourseParams{
		Title: title, Summary: strings.TrimSpace(r.FormValue("summary")), SortOrder: order, Art: art, ID: c.ID,
	}); err != nil {
		h.serverError(w, "update course", err)
		return
	}
	setFlashCookie(w, "Course saved.", "success")
	http.Redirect(w, r, "/admin/courses/"+strconv.FormatInt(c.ID, 10), http.StatusSeeOther)
}

// AdminCoursePublish publishes or unpublishes a course. On the first
// publish it can email everyone who gets Community email.
func (h *Handler) AdminCoursePublish(w http.ResponseWriter, r *http.Request) {
	c, ok := h.adminCourse(w, r)
	if !ok {
		return
	}
	publish := r.FormValue("publish") == "1"
	if publish {
		if lessons, _ := h.q.ListLessons(r.Context(), db.ListLessonsParams{CourseID: c.ID}); len(lessons) == 0 {
			setFlashCookie(w, "Add a lesson before publishing.", "error")
			http.Redirect(w, r, "/admin/courses/"+strconv.FormatInt(c.ID, 10), http.StatusSeeOther)
			return
		}
	}
	var on int64
	if publish {
		on = 1
	}
	if err := h.q.SetCoursePublished(r.Context(), db.SetCoursePublishedParams{Published: on, ID: c.ID}); err != nil {
		h.serverError(w, "publish course", err)
		return
	}
	msg := "Course unpublished. Only admins can see it."
	if publish {
		msg = "Course published."
		if !c.PublishedAt.Valid && r.FormValue("email") == "1" {
			msg += " Emailing " + strconv.Itoa(h.emailNewCourse(r, c)) + " members."
		}
	}
	setFlashCookie(w, msg, "success")
	http.Redirect(w, r, "/admin/courses/"+strconv.FormatInt(c.ID, 10), http.StatusSeeOther)
}

// emailNewCourse queues the new-course email to everyone with Community on.
func (h *Handler) emailNewCourse(r *http.Request, c db.Course) int {
	ids, err := h.q.ListActiveUserIDs(r.Context())
	if err != nil {
		slog.Error("list users for course email", "error", err)
		return 0
	}
	// The email gets the course's opening paragraph, not its list.
	intro := c.Summary
	if i := strings.Index(intro, "\n\n"); i >= 0 {
		intro = intro[:i]
	}
	data := struct{ Title, Summary, URL string }{c.Title, intro, coursePath(c.Slug, 0)}
	n := 0
	for _, id := range ids {
		queued, err := h.mailer.ToUser(r.Context(), id, mail.Community, "course_published", data)
		if err != nil {
			slog.Error("queue course email", "user", id, "error", err)
		}
		if queued {
			n++
		}
	}
	return n
}

// AdminCourseDelete deletes a course and its lessons.
func (h *Handler) AdminCourseDelete(w http.ResponseWriter, r *http.Request) {
	c, ok := h.adminCourse(w, r)
	if !ok {
		return
	}
	if err := h.q.DeleteCourse(r.Context(), c.ID); err != nil {
		h.serverError(w, "delete course", err)
		return
	}
	setFlashCookie(w, "Course deleted.", "success")
	http.Redirect(w, r, "/admin/courses", http.StatusSeeOther)
}

// lessonForm reads and checks the lesson form. It returns a message saying
// what's wrong, or "".
func lessonForm(r *http.Request) (title, description, videoURL, embed string, minutes, order int64, msg string) {
	title = strings.Join(strings.Fields(r.FormValue("title")), " ")
	description = strings.TrimSpace(r.FormValue("description"))
	videoURL = strings.TrimSpace(r.FormValue("video_url"))
	minutes, _ = strconv.ParseInt(r.FormValue("minutes"), 10, 64)
	order, _ = strconv.ParseInt(r.FormValue("sort_order"), 10, 64)
	embed, ok := embedURL(videoURL)
	switch {
	case title == "" || utf8.RuneCountInString(title) > 120:
		msg = "Give the lesson a title of up to 120 characters."
	case !ok:
		msg = "Paste a YouTube, Vimeo or Bunny Stream link for the video."
	case minutes < 0 || minutes > 600:
		msg = "Enter the video's length in minutes."
	}
	return title, description, videoURL, embed, minutes, order, msg
}

// AdminLessonCreate adds a lesson to the end of a course.
func (h *Handler) AdminLessonCreate(w http.ResponseWriter, r *http.Request) {
	c, ok := h.adminCourse(w, r)
	if !ok {
		return
	}
	back := "/admin/courses/" + strconv.FormatInt(c.ID, 10)
	title, description, videoURL, embed, minutes, _, msg := lessonForm(r)
	if msg != "" {
		setFlashCookie(w, msg, "error")
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	if _, err := h.q.CreateLesson(r.Context(), db.CreateLessonParams{
		CourseID: c.ID, Title: title, Description: description,
		VideoUrl: videoURL, EmbedUrl: embed, Minutes: minutes,
	}); err != nil {
		h.serverError(w, "create lesson", err)
		return
	}
	setFlashCookie(w, "Lesson added.", "success")
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// adminLesson loads the {lid} lesson.
func (h *Handler) adminLesson(w http.ResponseWriter, r *http.Request) (db.Lesson, bool) {
	id, err := strconv.ParseInt(r.PathValue("lid"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return db.Lesson{}, false
	}
	l, err := h.q.GetLesson(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return l, false
	}
	return l, true
}

// AdminLessonUpdate saves a lesson.
func (h *Handler) AdminLessonUpdate(w http.ResponseWriter, r *http.Request) {
	l, ok := h.adminLesson(w, r)
	if !ok {
		return
	}
	back := "/admin/courses/" + strconv.FormatInt(l.CourseID, 10)
	title, description, videoURL, embed, minutes, order, msg := lessonForm(r)
	if msg != "" {
		setFlashCookie(w, msg, "error")
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	if err := h.q.UpdateLesson(r.Context(), db.UpdateLessonParams{
		Title: title, Description: description, VideoUrl: videoURL, EmbedUrl: embed,
		Minutes: minutes, SortOrder: order, ID: l.ID,
	}); err != nil {
		h.serverError(w, "update lesson", err)
		return
	}
	setFlashCookie(w, "Lesson saved.", "success")
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// AdminLessonDelete deletes a lesson.
func (h *Handler) AdminLessonDelete(w http.ResponseWriter, r *http.Request) {
	l, ok := h.adminLesson(w, r)
	if !ok {
		return
	}
	if err := h.q.DeleteLesson(r.Context(), l.ID); err != nil {
		h.serverError(w, "delete lesson", err)
		return
	}
	setFlashCookie(w, "Lesson deleted.", "success")
	http.Redirect(w, r, "/admin/courses/"+strconv.FormatInt(l.CourseID, 10), http.StatusSeeOther)
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// slugify turns "Investing 101: The Basics" into "investing-101-the-basics".
func slugify(s string) string {
	slug := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(slug) > 60 {
		slug = strings.TrimRight(slug[:60], "-")
	}
	if slug == "" {
		slug = "course"
	}
	return slug
}
