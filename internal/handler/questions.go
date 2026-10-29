package handler

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/mail"
)

// Ask a Question: members post questions and vote for the ones they want
// answered; an admin (Rob) answers, which emails the asker and the voters.
const (
	maxQuestionTitle    = 150
	maxQuestionBody     = 2000
	maxAnswer           = 10000
	maxQuestionsPerDay  = 5
	questionAnsweredTpl = "question_answered"
)

// QuestionsPage lists questions, most votes first or newest first.
func (h *Handler) QuestionsPage(w http.ResponseWriter, r *http.Request) {
	h.renderQuestions(w, r, nil, "", "")
}

func (h *Handler) renderQuestions(w http.ResponseWriter, r *http.Request, errs map[string]string, title, body string) {
	user := auth.UserFromContext(r.Context())
	sort := r.URL.Query().Get("sort")
	if sort != "new" {
		sort = "top"
	}
	show := r.URL.Query().Get("show")
	if show != "open" && show != "answered" {
		show = "all"
	}
	questions, err := h.q.ListQuestions(r.Context(), db.ListQuestionsParams{Viewer: user.ID, Show: show})
	if err != nil {
		h.serverError(w, "list questions", err)
		return
	}
	if sort == "new" {
		slices.SortStableFunc(questions, func(a, b db.ListQuestionsRow) int {
			return strings.Compare(b.CreatedAt, a.CreatedAt)
		})
	}
	h.render(w, r, "questions/list", "", PageData{
		Title:  "Ask a Question",
		Items:  questions,
		Errors: errs,
		Extra: map[string]any{
			"Sort": sort, "Show": show,
			"Title": title, "Body": body,
		},
	})
}

// QuestionCreate posts a question. The asker's vote is added for them.
func (h *Handler) QuestionCreate(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	title := strings.Join(strings.Fields(r.FormValue("title")), " ")
	body := strings.TrimSpace(r.FormValue("body"))
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
		h.renderQuestions(w, r, errs, title, body)
		return
	}
	id, err := h.q.CreateQuestion(r.Context(), db.CreateQuestionParams{UserID: user.ID, Title: title, Body: body})
	if err != nil {
		h.serverError(w, "create question", err)
		return
	}
	_ = h.q.AddQuestionVote(r.Context(), db.AddQuestionVoteParams{QuestionID: id, UserID: user.ID})
	setFlashCookie(w, "Question posted. Others can vote for it now.", "success")
	http.Redirect(w, r, "/questions/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

// question loads the {qid} question as the current user sees it.
func (h *Handler) question(w http.ResponseWriter, r *http.Request) (db.GetQuestionRow, bool) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("qid"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return db.GetQuestionRow{}, false
	}
	q, err := h.q.GetQuestion(r.Context(), db.GetQuestionParams{Viewer: user.ID, ID: id})
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

// QuestionPage shows one question and its answer.
func (h *Handler) QuestionPage(w http.ResponseWriter, r *http.Request) {
	q, ok := h.question(w, r)
	if !ok {
		return
	}
	h.render(w, r, "questions/detail", "", PageData{Title: q.Title, Item: q})
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
		http.Redirect(w, r, "/questions/"+strconv.FormatInt(q.ID, 10), http.StatusSeeOther)
		return
	}
	if q, ok = h.question(w, r); ok {
		h.render(w, r, "questions/detail", "vote-response", PageData{Item: q})
	}
}

// QuestionAnswer saves an admin's answer. The first answer emails the asker
// and everyone who voted.
func (h *Handler) QuestionAnswer(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	q, ok := h.question(w, r)
	if !ok {
		return
	}
	answer := strings.TrimSpace(r.FormValue("answer"))
	if answer == "" || utf8.RuneCountInString(answer) > maxAnswer {
		setFlashCookie(w, "Write an answer of up to "+strconv.Itoa(maxAnswer)+" characters.", "error")
		http.Redirect(w, r, "/questions/"+strconv.FormatInt(q.ID, 10), http.StatusSeeOther)
		return
	}
	if err := h.q.AnswerQuestion(r.Context(), db.AnswerQuestionParams{
		Answer: answer, AnsweredBy: sql.NullInt64{Int64: user.ID, Valid: true}, ID: q.ID,
	}); err != nil {
		h.serverError(w, "answer question", err)
		return
	}
	msg := "Answer updated."
	if !q.AnsweredAt.Valid {
		n := h.emailAnswer(r, q, answer, user.Name)
		msg = "Answer posted. Emailing " + strconv.Itoa(n) + " people who asked or voted."
	}
	setFlashCookie(w, msg, "success")
	http.Redirect(w, r, "/questions/"+strconv.FormatInt(q.ID, 10), http.StatusSeeOther)
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
			URL:        "/questions/" + strconv.FormatInt(q.ID, 10),
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
	http.Redirect(w, r, "/questions", http.StatusSeeOther)
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
		http.Redirect(w, r, "/questions/"+strconv.FormatInt(q.ID, 10), http.StatusSeeOther)
		return
	}
	setFlashCookie(w, "Question deleted.", "success")
	http.Redirect(w, r, "/questions", http.StatusSeeOther)
}
