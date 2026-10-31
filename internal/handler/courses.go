package handler

import (
	"database/sql"
	"errors"
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

// Courses are pre-recorded video lessons for signed-in members. Admins build
// them as drafts on /admin/courses and publish when ready.

// CoursesPage lists the published courses with the viewer's progress.
func (h *Handler) CoursesPage(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	courses, err := h.q.ListPublishedCourses(r.Context(), user.ID)
	if err != nil {
		h.serverError(w, "list courses", err)
		return
	}
	h.render(w, r, "courses/list", "", learnPage(r, "courses", PageData{Title: "Courses", Items: courses}))
}

// course loads the {slug} course. Drafts are visible to admins only.
func (h *Handler) course(w http.ResponseWriter, r *http.Request) (db.Course, []db.ListLessonsRow, bool) {
	user := auth.UserFromContext(r.Context())
	c, err := h.q.GetCourseBySlug(r.Context(), r.PathValue("slug"))
	if errors.Is(err, sql.ErrNoRows) || (err == nil && c.Published == 0 && user.IsAdmin == 0) {
		http.NotFound(w, r)
		return c, nil, false
	}
	if err != nil {
		h.serverError(w, "get course", err)
		return c, nil, false
	}
	lessons, err := h.q.ListLessons(r.Context(), db.ListLessonsParams{Viewer: user.ID, CourseID: c.ID})
	if err != nil {
		h.serverError(w, "list lessons", err)
		return c, nil, false
	}
	return c, lessons, true
}

// CoursePage shows a course's lessons.
func (h *Handler) CoursePage(w http.ResponseWriter, r *http.Request) {
	c, lessons, ok := h.course(w, r)
	if !ok {
		return
	}
	done := 0
	for _, l := range lessons {
		if l.Completed {
			done++
		}
	}
	h.render(w, r, "courses/course", "", learnPage(r, "courses", PageData{
		Title: c.Title, Item: c, Items: lessons,
		Extra: map[string]any{"Done": done},
	}))
}

// LessonPage plays one lesson, with links to the lessons either side.
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
	base := "/courses/" + c.Slug + "/"
	if r.FormValue("done") == "0" {
		_ = h.q.UncompleteLesson(r.Context(), db.UncompleteLessonParams{UserID: user.ID, LessonID: lessons[i].ID})
		http.Redirect(w, r, base+strconv.FormatInt(lessons[i].ID, 10), http.StatusSeeOther)
		return
	}
	if err := h.q.CompleteLesson(r.Context(), db.CompleteLessonParams{UserID: user.ID, LessonID: lessons[i].ID}); err != nil {
		h.serverError(w, "complete lesson", err)
		return
	}
	if i+1 < len(lessons) {
		http.Redirect(w, r, base+strconv.FormatInt(lessons[i+1].ID, 10), http.StatusSeeOther)
		return
	}
	setFlashCookie(w, "You've finished "+c.Title+". Nice work.", "success")
	http.Redirect(w, r, "/courses/"+c.Slug, http.StatusSeeOther)
}

func lessonIndex(lessons []db.ListLessonsRow, id string) int {
	for i, l := range lessons {
		if strconv.FormatInt(l.ID, 10) == id {
			return i
		}
	}
	return -1
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
	h.render(w, r, "admin/course", "", PageData{Title: c.Title, Item: c, Items: lessons})
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
	if err := h.q.UpdateCourse(r.Context(), db.UpdateCourseParams{
		Title: title, Summary: strings.TrimSpace(r.FormValue("summary")), SortOrder: order, ID: c.ID,
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
	data := struct{ Title, Summary, URL string }{c.Title, c.Summary, "/courses/" + c.Slug}
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
	if err := h.q.CreateLesson(r.Context(), db.CreateLessonParams{
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
