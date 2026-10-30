package handler

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/mail"
	"github.com/exploded/game/internal/media"
)

// Ask a Question: members post questions and vote for the ones they want
// answered; an admin (Rob) answers in writing, with a video, or live on air,
// which emails the asker and the voters. It's the Questions tab of Learn, so
// anyone can read it.
const (
	maxQuestionTitle    = 150
	maxQuestionBody     = 2000
	maxAnswer           = 10000
	maxQuestionsPerDay  = 5
	questionAnsweredTpl = "question_answered"
	questionsPath       = "/learn/questions"
)

func questionPath(id int64) string { return questionsPath + "/" + strconv.FormatInt(id, 10) }

// QuestionsPage lists questions, most votes first or newest first.
func (h *Handler) QuestionsPage(w http.ResponseWriter, r *http.Request) {
	form := map[string]string{}
	// "Ask Rob about this" on a video page fills in the form.
	if vid, err := strconv.ParseInt(r.URL.Query().Get("video"), 10, 64); err == nil {
		if v, err := h.q.GetVideo(r.Context(), vid); err == nil && v.Hidden == 0 {
			form["Body"] = "About the video \"" + v.Title + "\": " + siteURL(r) + "/learn/v/" + strconv.FormatInt(v.ID, 10)
			if topics, _ := h.q.ListVideoTopics(r.Context(), v.ID); len(topics) > 0 {
				form["Topic"] = topics[0].Slug
			}
		}
	}
	h.renderQuestions(w, r, nil, form)
}

func (h *Handler) renderQuestions(w http.ResponseWriter, r *http.Request, errs map[string]string, form map[string]string) {
	ctx := r.Context()
	sort := r.URL.Query().Get("sort")
	if sort != "new" {
		sort = "top"
	}
	show := r.URL.Query().Get("show")
	if show != "open" && show != "answered" {
		show = "all"
	}
	topics, err := h.q.ListTopics(ctx)
	if err != nil {
		h.serverError(w, "list topics", err)
		return
	}
	topicSlug := r.URL.Query().Get("topic")
	var topicID int64
	for _, t := range topics {
		if t.Slug == topicSlug {
			topicID = t.ID
		}
	}
	if topicID == 0 {
		topicSlug = ""
	}
	questions, err := h.q.ListQuestions(ctx, db.ListQuestionsParams{Viewer: viewerID(r), Show: show, Topic: topicID})
	if err != nil {
		h.serverError(w, "list questions", err)
		return
	}
	if sort == "new" {
		slices.SortStableFunc(questions, func(a, b db.ListQuestionsRow) int {
			return strings.Compare(b.CreatedAt, a.CreatedAt)
		})
	}
	if form == nil {
		form = map[string]string{}
	}
	h.render(w, r, "learn/questions", "", learnPage(r, "questions", PageData{
		Title:  "Ask Rob",
		Items:  questions,
		Errors: errs,
		Extra: map[string]any{
			"Sort": sort, "Show": show, "TopicSlug": topicSlug, "Topics": topics,
			"Form": form,
		},
	}))
}

// QuestionCreate posts a question. The asker's vote is added for them. A
// question asked without a topic gets one from the classifier.
func (h *Handler) QuestionCreate(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	title := strings.Join(strings.Fields(r.FormValue("title")), " ")
	body := strings.TrimSpace(r.FormValue("body"))
	topicSlug := r.FormValue("topic")
	back := localPath(r.FormValue("back"), "")
	errs := map[string]string{}
	switch {
	case title == "":
		errs["title"] = "Enter your question."
	case utf8.RuneCountInString(title) > maxQuestionTitle:
		errs["title"] = "Keep the question to " + strconv.Itoa(maxQuestionTitle) + " characters. Put the rest in the details."
	}
	if utf8.RuneCountInString(body) > maxQuestionBody {
		errs["body"] = "Keep the details to " + strconv.Itoa(maxQuestionBody) + " characters."
	}
	if n, err := h.q.CountRecentQuestions(r.Context(), user.ID); err == nil && n >= maxQuestionsPerDay {
		errs["title"] = "You can ask " + strconv.Itoa(maxQuestionsPerDay) + " questions a day. Try again tomorrow."
	}
	if len(errs) > 0 {
		if back != "" {
			// Asked from a stream page: say what's wrong there.
			for _, msg := range errs {
				setFlashCookie(w, msg, "error")
				break
			}
			http.Redirect(w, r, back, http.StatusSeeOther)
			return
		}
		h.renderQuestions(w, r, errs, map[string]string{"Title": title, "Body": body, "Topic": topicSlug})
		return
	}
	var topicID sql.NullInt64
	if t, err := h.q.GetTopicBySlug(r.Context(), topicSlug); err == nil {
		topicID = nint64(t.ID)
	}
	id, err := h.q.CreateQuestion(r.Context(), db.CreateQuestionParams{UserID: user.ID, Title: title, Body: body, TopicID: topicID})
	if err != nil {
		h.serverError(w, "create question", err)
		return
	}
	_ = h.q.AddQuestionVote(r.Context(), db.AddQuestionVoteParams{QuestionID: id, UserID: user.ID})
	if !topicID.Valid {
		h.media.SuggestQuestionTopic(id, title, body)
	}
	if back != "" {
		setFlashCookie(w, "Question sent. It's in the queue for Rob.", "success")
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	setFlashCookie(w, "Question posted. Others can vote for it now.", "success")
	http.Redirect(w, r, questionPath(id), http.StatusSeeOther)
}

// question loads the {qid} question as the current viewer sees it.
func (h *Handler) question(w http.ResponseWriter, r *http.Request) (db.GetQuestionRow, bool) {
	id, err := strconv.ParseInt(r.PathValue("qid"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return db.GetQuestionRow{}, false
	}
	q, err := h.q.GetQuestion(r.Context(), db.GetQuestionParams{Viewer: viewerID(r), ID: id})
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return q, false
	}
	if err != nil {
		h.serverError(w, "get question", err)
		return q, false
	}
	return q, true
}

// QuestionPage shows one question and its answer. Admins get a list of
// videos to answer with (?vq= searches it).
func (h *Handler) QuestionPage(w http.ResponseWriter, r *http.Request) {
	q, ok := h.question(w, r)
	if !ok {
		return
	}
	extra := map[string]any{}
	if q.AnswerVideoID.Valid {
		platform := "youtube"
		if !strings.Contains(q.VideoEmbedUrl, "youtube") {
			platform = ""
		}
		extra["VideoEmbed"] = embedAt(q.VideoEmbedUrl, platform, q.AnswerSeconds)
	}
	if q.LiveEventID.Valid {
		if e, err := h.q.GetLiveEvent(r.Context(), q.LiveEventID.Int64); err == nil {
			extra["Event"] = e
		}
	}
	if u := auth.UserFromContext(r.Context()); u != nil && u.IsAdmin == 1 {
		vq := r.URL.Query().Get("vq")
		picks, _ := h.q.PickVideos(r.Context(), vq)
		extra["Picks"] = picks
		extra["VQ"] = vq
		live, _ := h.q.ListLiveNow(r.Context())
		extra["Live"] = live
	}
	h.render(w, r, "learn/question", "", learnPage(r, "questions", PageData{Title: q.Title, Item: q, Extra: extra}))
}

// QuestionVote toggles the user's vote. htmx swaps in the new button.
func (h *Handler) QuestionVote(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	q, ok := h.question(w, r)
	if !ok {
		return
	}
	params := db.AddQuestionVoteParams{QuestionID: q.ID, UserID: user.ID}
	var err error
	if q.Voted {
		err = h.q.RemoveQuestionVote(r.Context(), db.RemoveQuestionVoteParams(params))
	} else {
		err = h.q.AddQuestionVote(r.Context(), params)
	}
	if err != nil {
		h.serverError(w, "vote", err)
		return
	}
	if !isHTMX(r) {
		http.Redirect(w, r, localPath(r.FormValue("back"), questionPath(q.ID)), http.StatusSeeOther)
		return
	}
	if q, ok = h.question(w, r); ok {
		h.render(w, r, "learn/question", "vote-response", PageData{User: user, Item: q})
	}
}

// QuestionAnswer saves an admin's answer: text, a video from a given
// moment, or both. The first answer emails the asker and everyone who
// voted.
func (h *Handler) QuestionAnswer(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	q, ok := h.question(w, r)
	if !ok {
		return
	}
	back := questionPath(q.ID)
	answer := strings.TrimSpace(r.FormValue("answer"))
	videoID, _ := strconv.ParseInt(r.FormValue("video_id"), 10, 64)
	if r.FormValue("clear_video") == "1" {
		videoID = 0
	}
	if videoID > 0 {
		v, err := h.q.GetVideo(r.Context(), videoID)
		if err != nil {
			setFlashCookie(w, "That video isn't in the library.", "error")
			http.Redirect(w, r, back, http.StatusSeeOther)
			return
		}
		if answer == "" {
			answer = "Rob answers this in the video \"" + v.Title + "\"."
		}
	}
	if answer == "" || utf8.RuneCountInString(answer) > maxAnswer {
		setFlashCookie(w, "Write an answer of up to "+strconv.Itoa(maxAnswer)+" characters, or pick a video.", "error")
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	if err := h.q.AnswerQuestion(r.Context(), db.AnswerQuestionParams{
		Answer: answer, AnsweredBy: nint64(user.ID), ID: q.ID,
	}); err != nil {
		h.serverError(w, "answer question", err)
		return
	}
	if videoID > 0 || r.FormValue("clear_video") == "1" {
		var vid sql.NullInt64
		if videoID > 0 {
			vid = nint64(videoID)
		}
		if err := h.q.SetQuestionVideo(r.Context(), db.SetQuestionVideoParams{
			AnswerVideoID: vid, AnswerSeconds: parseTimestamp(r.FormValue("at")), ID: q.ID,
		}); err != nil {
			h.serverError(w, "answer with video", err)
			return
		}
	}
	msg := "Answer updated."
	if !q.AnsweredAt.Valid {
		n := h.emailAnswer(r, q, answer, user.Name)
		msg = "Answer posted. Emailing " + strconv.Itoa(n) + " people who asked or voted."
	}
	setFlashCookie(w, msg, "success")
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// parseTimestamp reads "90", "1:30" or "1:02:03" as seconds.
func parseTimestamp(s string) int64 {
	var total int64
	for _, part := range strings.Split(strings.TrimSpace(s), ":") {
		n, err := strconv.ParseInt(part, 10, 64)
		if err != nil || n < 0 {
			return 0
		}
		total = total*60 + n
	}
	return total
}

// QuestionOnAir marks a question answered during a live stream, at the
// current point in it. Followers are emailed when the stream ends.
func (h *Handler) QuestionOnAir(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	q, ok := h.question(w, r)
	if !ok {
		return
	}
	eventID, _ := strconv.ParseInt(r.FormValue("event_id"), 10, 64)
	e, err := h.q.GetLiveEvent(r.Context(), eventID)
	if err != nil {
		setFlashCookie(w, "That stream doesn't exist.", "error")
		http.Redirect(w, r, questionPath(q.ID), http.StatusSeeOther)
		return
	}
	var at int64
	if started := parseUTC(e.StartedAt.String); e.StartedAt.Valid && !started.IsZero() {
		// A little before now: Rob reads the question out before tapping.
		at = max(int64(time.Since(started).Seconds())-60, 0)
	}
	answer := "Rob answered this live on " + media.PlatformName(e.Platform) + " (" + e.Title + ")."
	if err := h.q.AnswerOnAir(r.Context(), db.AnswerOnAirParams{
		LiveEventID: nint64(e.ID), AnswerSeconds: at, Answer: answer,
		AnsweredBy: nint64(user.ID), ID: q.ID,
	}); err != nil {
		h.serverError(w, "answer on air", err)
		return
	}
	if isHTMX(r) {
		queue, _ := h.q.ListLiveQueue(r.Context(), viewerID(r))
		triggerToast(w, "Marked answered. Voters get an email after the stream.", "success")
		h.render(w, r, "learn/event", "queue", learnPage(r, "live", PageData{User: user, Item: e, Extra: map[string]any{"Queue": queue}}))
		return
	}
	setFlashCookie(w, "Marked answered on air. Voters get an email after the stream.", "success")
	http.Redirect(w, r, localPath(r.FormValue("back"), questionPath(q.ID)), http.StatusSeeOther)
}

// answeredEmail is the data for question_answered.tmpl.
type answeredEmail struct {
	Title      string
	URL        string
	AnsweredBy string
	Asker      bool
	Answer     string
}

// emailAnswer queues the answer email and returns how many were queued.
func (h *Handler) emailAnswer(r *http.Request, q db.GetQuestionRow, answer, answeredBy string) int {
	followers, err := h.q.ListQuestionFollowers(r.Context(), q.ID)
	if err != nil {
		slog.Error("list question followers", "question", q.ID, "error", err)
		return 0
	}
	n := 0
	for _, id := range followers {
		queued, err := h.mailer.ToUser(r.Context(), id, mail.Community, questionAnsweredTpl, answeredEmail{
			Title:      q.Title,
			URL:        questionPath(q.ID),
			AnsweredBy: answeredBy,
			Asker:      id == q.UserID,
			Answer:     answer,
		})
		if err != nil {
			slog.Error("queue answer email", "question", q.ID, "user", id, "error", err)
		}
		if queued {
			n++
		}
	}
	return n
}

// QuestionHide removes a question from the list (admins only).
func (h *Handler) QuestionHide(w http.ResponseWriter, r *http.Request) {
	q, ok := h.question(w, r)
	if !ok {
		return
	}
	if err := h.q.HideQuestion(r.Context(), q.ID); err != nil {
		h.serverError(w, "hide question", err)
		return
	}
	setFlashCookie(w, "Question removed.", "success")
	http.Redirect(w, r, questionsPath, http.StatusSeeOther)
}

// QuestionDelete lets the asker delete their question until it's answered.
func (h *Handler) QuestionDelete(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	q, ok := h.question(w, r)
	if !ok {
		return
	}
	n, err := h.q.DeleteOwnQuestion(r.Context(), db.DeleteOwnQuestionParams{ID: q.ID, UserID: user.ID})
	if err != nil {
		h.serverError(w, "delete question", err)
		return
	}
	if n == 0 {
		setFlashCookie(w, "You can only delete your own questions, and only before they're answered.", "error")
		http.Redirect(w, r, questionPath(q.ID), http.StatusSeeOther)
		return
	}
	setFlashCookie(w, "Question deleted.", "success")
	http.Redirect(w, r, questionsPath, http.StatusSeeOther)
}

// OldQuestionsRedirect sends the old /questions URLs to Learn.
func OldQuestionsRedirect(w http.ResponseWriter, r *http.Request) {
	target := "/learn" + r.URL.Path
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, target, http.StatusMovedPermanently)
}
