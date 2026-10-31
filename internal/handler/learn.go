package handler

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/media"
)

// Learn is the public hub for Rob's videos, live streams and questions.
// Anyone can browse; asking, voting and reminders need an account.

// social is one of Rob's accounts, linked from the Watch page.
type social struct{ Name, URL, Class string }

var robSocials = []social{
	{"YouTube", "https://www.youtube.com/@ConsortiumPW", "yt"},
	{"TikTok", "https://www.tiktok.com/@rob_goudie", "tt"},
	{"Instagram", "https://www.instagram.com/consortiumprivatewealth/", "ig"},
	{"Facebook", "https://www.facebook.com/consortiumpw", "fb"},
	{"X", "https://x.com/consortiumpw", "x"},
}

// learnDisclaimer is the general advice warning under every Learn page:
// Rob's licensee's wording, word for word. LEARN_DISCLAIMER replaces it.
func learnDisclaimer() string {
	if d := os.Getenv("LEARN_DISCLAIMER"); d != "" {
		return d
	}
	return "The information provided in this article is general in nature only and does not " +
		"constitute personal financial advice."
}

// learnPage fills in what every Learn page shows: the tab, the warning, and
// a sign-in link that comes back to this page.
func learnPage(r *http.Request, tab string, d PageData) PageData {
	if d.Extra == nil {
		d.Extra = map[string]any{}
	}
	d.Extra["Tab"] = tab
	d.Extra["Disclaimer"] = learnDisclaimer()
	d.Extra["SignIn"] = "/login?next=" + url.QueryEscape(r.URL.RequestURI())
	return d
}

// viewerID is the signed-in user's id, or 0 for a visitor.
func viewerID(r *http.Request) int64 {
	if u := auth.UserFromContext(r.Context()); u != nil {
		return u.ID
	}
	return 0
}

// topicRow is one topic's row of videos on the Watch page.
type topicRow struct {
	Topic  db.ListTopicsRow
	Videos []db.Video
}

// LearnHome is the Watch tab: what's live, then rows of videos.
func (h *Handler) LearnHome(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	topics, err := h.q.ListTopics(ctx)
	if err != nil {
		h.serverError(w, "list topics", err)
		return
	}
	extra := map[string]any{"Topics": topics, "Socials": robSocials}

	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		results, err := h.q.SearchVideos(ctx, q)
		if err != nil {
			h.serverError(w, "search videos", err)
			return
		}
		lower := strings.ToLower(q)
		slices.SortStableFunc(results, func(a, b db.Video) int {
			return boolRank(strings.Contains(strings.ToLower(b.Title), lower)) - boolRank(strings.Contains(strings.ToLower(a.Title), lower))
		})
		extra["Query"] = q
		extra["Results"] = results
		h.render(w, r, "learn/home", "", learnPage(r, "watch", PageData{Title: "Search: " + q, Extra: extra}))
		return
	}

	latest, err := h.q.ListLatestVideos(ctx, db.ListLatestVideosParams{Vertical: 0, Lim: 12})
	if err != nil {
		h.serverError(w, "latest videos", err)
		return
	}
	shorts, _ := h.q.ListLatestVideos(ctx, db.ListLatestVideosParams{Vertical: 1, Lim: 16})
	featured, _ := h.q.ListFeaturedVideos(ctx)
	upcoming, _ := h.q.ListUpcomingLive(ctx)
	var rows []topicRow
	for _, t := range topics {
		if t.Videos == 0 {
			continue
		}
		videos, err := h.q.ListTopicVideos(ctx, db.ListTopicVideosParams{TopicID: t.ID, Lim: 12})
		if err != nil {
			h.serverError(w, "topic videos", err)
			return
		}
		rows = append(rows, topicRow{Topic: t, Videos: videos})
	}
	if len(upcoming) > 0 {
		extra["Next"] = upcoming[0]
	}
	extra["Latest"] = latest
	extra["Shorts"] = shorts
	extra["Featured"] = featured
	extra["Rows"] = rows
	h.render(w, r, "learn/home", "", learnPage(r, "watch", PageData{Title: "Learn with Rob", Extra: extra}))
}

// learnPerPage is how many videos a topic page shows at a time.
const learnPerPage = 24

// LearnTopic lists one topic's videos and questions.
func (h *Handler) LearnTopic(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	t, err := h.q.GetTopicBySlug(ctx, r.PathValue("slug"))
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.serverError(w, "get topic", err)
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	page = max(page, 1)
	videos, err := h.q.ListTopicVideos(ctx, db.ListTopicVideosParams{
		TopicID: t.ID, Lim: learnPerPage, Off: int64((page - 1) * learnPerPage),
	})
	if err != nil {
		h.serverError(w, "topic videos", err)
		return
	}
	total, _ := h.q.CountTopicVideos(ctx, t.ID)
	questions, _ := h.q.ListTopicQuestions(ctx, nint64(t.ID))
	topics, _ := h.q.ListTopics(ctx)
	extra := map[string]any{
		"Topic": t, "Topics": topics, "Questions": questions, "Total": total, "Page": page,
	}
	if page > 1 {
		extra["Prev"] = page - 1
	}
	if int64(page*learnPerPage) < total {
		extra["Next"] = page + 1
	}
	h.render(w, r, "learn/topic", "", learnPage(r, "watch", PageData{Title: t.Name, Items: videos, Extra: extra}))
}

// learnVideo loads the {vid} video. Hidden videos are for admins only.
func (h *Handler) learnVideo(w http.ResponseWriter, r *http.Request) (db.Video, bool) {
	id, err := strconv.ParseInt(r.PathValue("vid"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return db.Video{}, false
	}
	v, err := h.q.GetVideo(r.Context(), id)
	u := auth.UserFromContext(r.Context())
	if errors.Is(err, sql.ErrNoRows) || (err == nil && v.Hidden == 1 && (u == nil || u.IsAdmin == 0)) {
		http.NotFound(w, r)
		return v, false
	}
	if err != nil {
		h.serverError(w, "get video", err)
		return v, false
	}
	return v, true
}

// LearnVideo plays one video. ?t=90 starts a YouTube video 90 seconds in.
func (h *Handler) LearnVideo(w http.ResponseWriter, r *http.Request) {
	v, ok := h.learnVideo(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	topics, _ := h.q.ListVideoTopics(ctx, v.ID)
	related, _ := h.q.ListRelatedVideos(ctx, v.ID)
	var stocks []db.ListVideoStocksRow
	if v.HideStocks == 0 {
		stocks, _ = h.q.ListVideoStocks(ctx, v.ID)
	}
	start, _ := strconv.ParseInt(r.URL.Query().Get("t"), 10, 64)
	h.render(w, r, "learn/video", "", learnPage(r, "watch", PageData{
		Title: v.Title,
		Item:  v,
		Extra: map[string]any{
			"Topics":  topics,
			"Stocks":  stocks,
			"Related": related,
			"Embed":   embedAt(v.EmbedUrl, v.Platform, start),
			"Start":   start,
		},
	}))
}

// embedAt adds a start time to a YouTube player URL. Other players can't
// start part-way.
func embedAt(embed, platform string, seconds int64) string {
	if platform != "youtube" || seconds <= 0 {
		return embed
	}
	sep := "?"
	if strings.Contains(embed, "?") {
		sep = "&"
	}
	return embed + sep + "start=" + strconv.FormatInt(seconds, 10)
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

// placeholderThumb is shown for a video with no cover.
const placeholderThumb = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 9"><rect width="16" height="9" fill="#1C2230"/><path d="M6.5 2.75v3.5L9.5 4.5z" fill="#878A9B"/></svg>`

// LearnThumb serves a cached cover (TikTok covers expire, so they're kept).
func (h *Handler) LearnThumb(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("vid"), 10, 64)
	t, err := h.q.GetVideoThumb(r.Context(), id)
	if err != nil {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		fmt.Fprint(w, placeholderThumb)
		return
	}
	w.Header().Set("Content-Type", t.ContentType)
	w.Header().Set("Cache-Control", "public, max-age=604800")
	w.Write(t.Data)
}

// LearnLive is the Live tab: streams on now, coming up and past.
func (h *Handler) LearnLive(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now, _ := h.q.ListLiveNow(ctx)
	upcoming, _ := h.q.ListUpcomingLive(ctx)
	past, _ := h.q.ListPastLive(ctx, 24)
	h.render(w, r, "learn/live", "", learnPage(r, "live", PageData{
		Title: "Live with Rob",
		Extra: map[string]any{
			"Now": now, "Upcoming": upcoming, "Past": past,
			"Reminders": h.reminders(r),
		},
	}))
}

// reminders is the set of streams the viewer asked to be reminded about.
func (h *Handler) reminders(r *http.Request) map[int64]bool {
	set := map[int64]bool{}
	if id := viewerID(r); id != 0 {
		ids, _ := h.q.ListUserLiveReminders(r.Context(), id)
		for _, e := range ids {
			set[e] = true
		}
	}
	return set
}

// liveEvent loads the {eid} stream.
func (h *Handler) liveEvent(w http.ResponseWriter, r *http.Request) (db.LiveEvent, bool) {
	id, err := strconv.ParseInt(r.PathValue("eid"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return db.LiveEvent{}, false
	}
	e, err := h.q.GetLiveEvent(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return e, false
	}
	if err != nil {
		h.serverError(w, "get stream", err)
		return e, false
	}
	return e, true
}

// LearnLiveEvent is one stream's page: the player and the Q&A queue.
func (h *Handler) LearnLiveEvent(w http.ResponseWriter, r *http.Request) {
	e, ok := h.liveEvent(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	queue, _ := h.q.ListLiveQueue(ctx, viewerID(r))
	answered, _ := h.q.ListLiveAnswered(ctx, nint64(e.ID))
	extra := map[string]any{
		"Queue":    queue,
		"Answered": answered,
		"Reminded": h.reminders(r)[e.ID],
	}
	if e.VideoID.Valid {
		if v, err := h.q.GetVideo(ctx, e.VideoID.Int64); err == nil {
			extra["Video"] = v
		}
	}
	h.render(w, r, "learn/event", "", learnPage(r, "live", PageData{Title: e.Title, Item: e, Extra: extra}))
}

// LearnLiveQueue is the queue on its own, which the stream page polls.
func (h *Handler) LearnLiveQueue(w http.ResponseWriter, r *http.Request) {
	e, ok := h.liveEvent(w, r)
	if !ok {
		return
	}
	queue, _ := h.q.ListLiveQueue(r.Context(), viewerID(r))
	h.render(w, r, "learn/event", "queue", learnPage(r, "live", PageData{
		User: auth.UserFromContext(r.Context()), Item: e, Extra: map[string]any{"Queue": queue},
	}))
}

// LearnLiveRemind turns the viewer's reminder for a stream on or off.
func (h *Handler) LearnLiveRemind(w http.ResponseWriter, r *http.Request) {
	e, ok := h.liveEvent(w, r)
	if !ok {
		return
	}
	user := auth.UserFromContext(r.Context())
	on := r.FormValue("on") == "1"
	var err error
	if on {
		err = h.q.AddLiveReminder(r.Context(), db.AddLiveReminderParams{EventID: e.ID, UserID: user.ID})
	} else {
		err = h.q.RemoveLiveReminder(r.Context(), db.RemoveLiveReminderParams{EventID: e.ID, UserID: user.ID})
	}
	if err != nil {
		h.serverError(w, "stream reminder", err)
		return
	}
	if isHTMX(r) {
		h.render(w, r, "learn/event", "remind", PageData{User: user, Item: e, Extra: map[string]any{"Reminded": on}})
		return
	}
	if on {
		setFlashCookie(w, "We'll email you when it starts.", "success")
	}
	http.Redirect(w, r, localPath(r.FormValue("back"), "/learn/live/"+strconv.FormatInt(e.ID, 10)), http.StatusSeeOther)
}

// LearnLiveCalendar is the stream as an .ics file for a calendar app.
func (h *Handler) LearnLiveCalendar(w http.ResponseWriter, r *http.Request) {
	e, ok := h.liveEvent(w, r)
	if !ok {
		return
	}
	start := parseUTC(e.ScheduledAt.String)
	if !e.ScheduledAt.Valid || start.IsZero() {
		http.NotFound(w, r)
		return
	}
	page := siteURL(r) + "/learn/live/" + strconv.FormatInt(e.ID, 10)
	const stamp = "20060102T150405Z"
	lines := []string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:-//The Wealth Lab//Learn//EN",
		"METHOD:PUBLISH",
		"BEGIN:VEVENT",
		"UID:live-" + strconv.FormatInt(e.ID, 10) + "@" + r.Host,
		"DTSTAMP:" + time.Now().UTC().Format(stamp),
		"DTSTART:" + start.UTC().Format(stamp),
		"DTEND:" + start.Add(time.Hour).UTC().Format(stamp),
		"SUMMARY:" + icsText("Rob live on "+media.PlatformName(e.Platform)+": "+e.Title),
		"DESCRIPTION:" + icsText("Ask questions and vote: "+page+"\nWatch: "+e.Url),
		"URL:" + page,
		"END:VEVENT",
		"END:VCALENDAR",
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="rob-live.ics"`)
	fmt.Fprint(w, strings.Join(lines, "\r\n")+"\r\n")
}

// icsText escapes a calendar text value.
func icsText(s string) string {
	return strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r", "", "\n", `\n`).Replace(s)
}

// siteURL is BASE_URL, or this request's origin when it isn't set.
func siteURL(r *http.Request) string {
	if base := strings.TrimRight(os.Getenv("BASE_URL"), "/"); base != "" {
		return base
	}
	scheme := "https"
	if r.TLS == nil && !SecurityHeadersProd {
		scheme = "http"
	}
	return scheme + "://" + r.Host
}

// parseUTC reads a time stored by SQLite's datetime().
func parseUTC(s string) time.Time {
	t, err := time.Parse(time.DateTime, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// localPath returns p if it's a path on this site, otherwise fallback. It
// keeps redirects (next=, back=) from sending people to another site.
func localPath(p, fallback string) string {
	if p == "" || !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.HasPrefix(p, "/\\") {
		return fallback
	}
	u, err := url.Parse(p)
	if err != nil || u.Scheme != "" || u.Host != "" {
		return fallback
	}
	return p
}

// LearnCourses sends the Courses tab to the courses page, which is for
// members.
func (h *Handler) LearnCourses(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/courses", http.StatusSeeOther)
}

// liveNow is the stream for the site-wide "Rob is live" banner, if any.
func (h *Handler) liveNow(r *http.Request) *db.LiveEvent {
	if h.q == nil {
		return nil // page tests run without a database
	}
	live, err := h.q.ListLiveNow(r.Context())
	if err != nil || len(live) == 0 {
		return nil
	}
	return &live[0]
}
