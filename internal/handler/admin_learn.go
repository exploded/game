package handler

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/media"
)

// The Learn admin pages: the video library, topics, live streams and the
// TikTok connection.

const adminLearnPerPage = 50

// AdminLearn lists the video library with the sync status and a form to
// paste a link.
func (h *Handler) AdminLearn(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	platform := r.URL.Query().Get("platform")
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	page = max(page, 1)
	videos, err := h.q.ListVideosAdmin(ctx, db.ListVideosAdminParams{
		Platform: platform, Q: q, Lim: adminLearnPerPage, Off: int64((page - 1) * adminLearnPerPage),
	})
	if err != nil {
		h.serverError(w, "list videos", err)
		return
	}
	counts, _ := h.q.CountVideosByPlatform(ctx)
	extra := map[string]any{
		"Platform": platform, "Q": q, "Page": page,
		"Counts": counts, "Status": h.media.Status(),
	}
	if tok, err := h.q.GetOAuthToken(ctx, "tiktok"); err == nil {
		extra["TikTok"] = tok
		extra["TikTokExpires"] = parseUTC(tok.RefreshExpiresAt)
		extra["TikTokExpiring"] = time.Until(parseUTC(tok.RefreshExpiresAt)) < 30*24*time.Hour
	}
	if page > 1 {
		extra["Prev"] = page - 1
	}
	if len(videos) == adminLearnPerPage {
		extra["Next"] = page + 1
	}
	h.render(w, r, "admin/learn", "", PageData{Title: "Learn", Items: videos, Extra: extra})
}

// AdminLearnSync starts the YouTube and TikTok syncs now.
func (h *Handler) AdminLearnSync(w http.ResponseWriter, r *http.Request) {
	if !h.media.Status().YouTube && !h.media.Status().TikTok {
		setFlashCookie(w, "Nothing to sync: set YOUTUBE_API_KEY (and the TikTok keys) in .env.", "error")
	} else {
		h.media.SyncNow(context.WithoutCancel(r.Context()), r.FormValue("full") == "1")
		setFlashCookie(w, "Sync started. New videos appear in a minute or two; topics follow as Claude sorts them.", "success")
	}
	http.Redirect(w, r, "/admin/learn", http.StatusSeeOther)
}

// AdminLearnPaste adds a video from a pasted link.
func (h *Handler) AdminLearnPaste(w http.ResponseWriter, r *http.Request) {
	link := media.ExpandShortLink(r.Context(), r.FormValue("url"))
	v, ok := parseVideo(link)
	if !ok || v.Platform == "vimeo" || v.Platform == "bunny" {
		setFlashCookie(w, "Paste a YouTube, TikTok, Facebook or Instagram video link.", "error")
		http.Redirect(w, r, "/admin/learn", http.StatusSeeOther)
		return
	}
	published, _ := time.ParseInLocation("2006-01-02", r.FormValue("published"), displayLoc)
	id, err := h.media.AddPasted(r.Context(), media.Pasted{
		Platform: v.Platform, ID: v.ID, URL: strings.TrimSpace(link), Embed: v.Embed,
		Vertical: v.Vertical, Title: r.FormValue("title"), Published: published,
	})
	if err != nil {
		setFlashCookie(w, "Couldn't add it: "+err.Error()+".", "error")
		http.Redirect(w, r, "/admin/learn", http.StatusSeeOther)
		return
	}
	setFlashCookie(w, "Video added. Claude picks its topics within a minute, or set them here.", "success")
	http.Redirect(w, r, "/admin/learn/videos/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

// adminVideo loads the {vid} video for the admin pages.
func (h *Handler) adminVideo(w http.ResponseWriter, r *http.Request) (db.Video, bool) {
	id, err := strconv.ParseInt(r.PathValue("vid"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return db.Video{}, false
	}
	v, err := h.q.GetVideo(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return v, false
	}
	return v, true
}

// AdminLearnVideo edits a video's topics and whether it's shown or pinned.
func (h *Handler) AdminLearnVideo(w http.ResponseWriter, r *http.Request) {
	v, ok := h.adminVideo(w, r)
	if !ok {
		return
	}
	topics, _ := h.q.ListTopics(r.Context())
	picked, _ := h.q.ListVideoTopics(r.Context(), v.ID)
	set := map[int64]string{}
	for _, t := range picked {
		set[t.ID] = t.Source
	}
	stocks, _ := h.q.ListVideoStocks(r.Context(), v.ID)
	h.render(w, r, "admin/learn_video", "", PageData{
		Title: v.Title, Item: v,
		Extra: map[string]any{"Topics": topics, "Picked": set, "Stocks": stocks},
	})
}

// AdminLearnVideoUpdate saves the topics ticked (as the admin's, so the
// classifier leaves them alone), hidden, featured and hide_stocks.
func (h *Handler) AdminLearnVideoUpdate(w http.ResponseWriter, r *http.Request) {
	v, ok := h.adminVideo(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	want := map[int64]bool{}
	for _, s := range r.Form["topic"] {
		if id, err := strconv.ParseInt(s, 10, 64); err == nil {
			want[id] = true
		}
	}
	topics, _ := h.q.ListTopics(ctx)
	for _, t := range topics {
		// Re-adding as the admin's marks Claude's picks as checked.
		_ = h.q.RemoveVideoTopic(ctx, db.RemoveVideoTopicParams{VideoID: v.ID, TopicID: t.ID})
		if want[t.ID] {
			if err := h.q.AddVideoTopic(ctx, db.AddVideoTopicParams{VideoID: v.ID, TopicID: t.ID, Source: "admin"}); err != nil {
				h.serverError(w, "set video topic", err)
				return
			}
		}
	}
	_ = h.q.SetVideoHidden(ctx, db.SetVideoHiddenParams{Hidden: boolFlag(r.FormValue("hidden")), ID: v.ID})
	_ = h.q.SetVideoFeatured(ctx, db.SetVideoFeaturedParams{Featured: boolFlag(r.FormValue("featured")), ID: v.ID})
	_ = h.q.SetVideoHideStocks(ctx, db.SetVideoHideStocksParams{HideStocks: boolFlag(r.FormValue("hide_stocks")), ID: v.ID})
	_ = h.q.SetVideoClassified(ctx, v.ID)
	setFlashCookie(w, "Video saved.", "success")
	http.Redirect(w, r, "/admin/learn/videos/"+strconv.FormatInt(v.ID, 10), http.StatusSeeOther)
}

func boolFlag(s string) int64 {
	if s != "" && s != "0" {
		return 1
	}
	return 0
}

// AdminLearnVideoReclassify asks Claude again (the admin's topics stay).
func (h *Handler) AdminLearnVideoReclassify(w http.ResponseWriter, r *http.Request) {
	v, ok := h.adminVideo(w, r)
	if !ok {
		return
	}
	if err := h.media.ClassifyVideo(r.Context(), v.ID); err != nil {
		setFlashCookie(w, "Couldn't sort it: "+err.Error(), "error")
	} else {
		setFlashCookie(w, "Topics and stocks updated.", "success")
	}
	http.Redirect(w, r, "/admin/learn/videos/"+strconv.FormatInt(v.ID, 10), http.StatusSeeOther)
}

// AdminLearnVideoDelete removes a video. A synced one comes back on the
// next sync unless it's gone from the platform, so hiding is usually better.
func (h *Handler) AdminLearnVideoDelete(w http.ResponseWriter, r *http.Request) {
	v, ok := h.adminVideo(w, r)
	if !ok {
		return
	}
	if err := h.q.DeleteVideo(r.Context(), v.ID); err != nil {
		h.serverError(w, "delete video", err)
		return
	}
	setFlashCookie(w, "Video deleted.", "success")
	http.Redirect(w, r, "/admin/learn", http.StatusSeeOther)
}

// AdminLearnTopics lists the topics with a form to add one.
func (h *Handler) AdminLearnTopics(w http.ResponseWriter, r *http.Request) {
	topics, err := h.q.ListTopics(r.Context())
	if err != nil {
		h.serverError(w, "list topics", err)
		return
	}
	h.render(w, r, "admin/learn_topics", "", PageData{Title: "Topics", Items: topics})
}

// topicForm reads the topic form, returning a message saying what's wrong
// or "".
func topicForm(r *http.Request) (name, description string, order int64, msg string) {
	name = strings.Join(strings.Fields(r.FormValue("name")), " ")
	description = strings.TrimSpace(r.FormValue("description"))
	order, _ = strconv.ParseInt(r.FormValue("sort_order"), 10, 64)
	switch {
	case name == "" || utf8.RuneCountInString(name) > 60:
		msg = "Give the topic a name of up to 60 characters."
	case utf8.RuneCountInString(description) > 400:
		msg = "Keep the description to 400 characters."
	}
	return
}

// AdminLearnTopicCreate adds a topic. Videos are sorted into it as they're
// classified; reclassify older ones from their pages.
func (h *Handler) AdminLearnTopicCreate(w http.ResponseWriter, r *http.Request) {
	name, description, _, msg := topicForm(r)
	if msg == "" {
		if _, err := h.q.GetTopicBySlug(r.Context(), slugify(name)); err == nil {
			msg = "There's already a topic called that."
		}
	}
	if msg != "" {
		setFlashCookie(w, msg, "error")
		http.Redirect(w, r, "/admin/learn/topics", http.StatusSeeOther)
		return
	}
	if err := h.q.CreateTopic(r.Context(), db.CreateTopicParams{Slug: slugify(name), Name: name, Description: description}); err != nil {
		h.serverError(w, "create topic", err)
		return
	}
	setFlashCookie(w, "Topic added.", "success")
	http.Redirect(w, r, "/admin/learn/topics", http.StatusSeeOther)
}

// AdminLearnTopicUpdate renames a topic. Its slug, and so its URL, stays.
func (h *Handler) AdminLearnTopicUpdate(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("tid"), 10, 64)
	name, description, order, msg := topicForm(r)
	if msg != "" {
		setFlashCookie(w, msg, "error")
	} else if err := h.q.UpdateTopic(r.Context(), db.UpdateTopicParams{Name: name, Description: description, SortOrder: order, ID: id}); err != nil {
		h.serverError(w, "update topic", err)
		return
	} else {
		setFlashCookie(w, "Topic saved.", "success")
	}
	http.Redirect(w, r, "/admin/learn/topics", http.StatusSeeOther)
}

// AdminLearnTopicDelete deletes a topic. Its videos stay, in other topics.
func (h *Handler) AdminLearnTopicDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("tid"), 10, 64)
	if err := h.q.DeleteTopic(r.Context(), id); err != nil {
		h.serverError(w, "delete topic", err)
		return
	}
	setFlashCookie(w, "Topic deleted.", "success")
	http.Redirect(w, r, "/admin/learn/topics", http.StatusSeeOther)
}

// AdminLearnLive lists streams and adds one by hand (TikTok, Facebook and
// Instagram have no schedule API; YouTube streams appear by themselves).
func (h *Handler) AdminLearnLive(w http.ResponseWriter, r *http.Request) {
	events, err := h.q.ListAllLiveEvents(r.Context())
	if err != nil {
		h.serverError(w, "list streams", err)
		return
	}
	h.render(w, r, "admin/learn_live", "", PageData{Title: "Live streams", Items: events})
}

// liveForm reads the stream form. scheduled is "" for "now".
func liveForm(r *http.Request) (platform, link, embed, title, description string, scheduled sql.NullString, msg string) {
	platform = r.FormValue("platform")
	link = strings.TrimSpace(r.FormValue("url"))
	title = strings.Join(strings.Fields(r.FormValue("title")), " ")
	description = strings.TrimSpace(r.FormValue("description"))
	if v, ok := parseVideo(link); ok {
		platform, embed = v.Platform, v.Embed
	}
	switch platform {
	case "youtube", "tiktok", "facebook", "instagram", "x":
	default:
		msg = "Pick where the stream is."
	}
	if when := r.FormValue("scheduled"); when != "" {
		t, err := time.ParseInLocation("2006-01-02T15:04", when, displayLoc)
		if err != nil {
			msg = "Enter the start time as a date and time."
		} else {
			scheduled = sql.NullString{String: t.UTC().Format(time.DateTime), Valid: true}
		}
	}
	switch {
	case title == "" || utf8.RuneCountInString(title) > 150:
		msg = "Give the stream a title of up to 150 characters."
	case !strings.HasPrefix(link, "https://"):
		msg = "Paste the link people use to watch (it starts with https://)."
	}
	return
}

// AdminLearnLiveCreate adds a stream. "Live now" starts it straight away,
// which shows the banner and emails the reminders.
func (h *Handler) AdminLearnLiveCreate(w http.ResponseWriter, r *http.Request) {
	platform, link, embed, title, description, scheduled, msg := liveForm(r)
	now := r.FormValue("now") == "1"
	if !now && !scheduled.Valid && msg == "" {
		msg = "Enter when it starts, or choose Live now."
	}
	if msg != "" {
		setFlashCookie(w, msg, "error")
		http.Redirect(w, r, "/admin/learn/live", http.StatusSeeOther)
		return
	}
	if now && !scheduled.Valid {
		scheduled = sql.NullString{String: time.Now().UTC().Format(time.DateTime), Valid: true}
	}
	id, err := h.q.CreateLiveEvent(r.Context(), db.CreateLiveEventParams{
		Platform: platform, Url: link, EmbedUrl: embed, Title: title, Description: description,
		ScheduledAt: scheduled, Status: "upcoming",
	})
	if err != nil {
		h.serverError(w, "create stream", err)
		return
	}
	if now {
		// Announcing a stream that's already on would only arrive late.
		_ = h.q.MarkLiveAnnounced(r.Context(), id)
		_ = h.q.GoLive(r.Context(), id)
	}
	setFlashCookie(w, "Stream added.", "success")
	http.Redirect(w, r, "/learn/live/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

// AdminLearnLiveAction updates, starts, ends, cancels or deletes a stream.
func (h *Handler) AdminLearnLiveAction(w http.ResponseWriter, r *http.Request) {
	e, ok := h.liveEvent(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	back := "/learn/live/" + strconv.FormatInt(e.ID, 10)
	var err error
	msg := ""
	switch r.PathValue("action") {
	case "save":
		platform, link, embed, title, description, scheduled, bad := liveForm(r)
		if bad != "" {
			setFlashCookie(w, bad, "error")
			http.Redirect(w, r, back, http.StatusSeeOther)
			return
		}
		err = h.q.UpdateLiveEvent(ctx, db.UpdateLiveEventParams{
			Platform: platform, Url: link, EmbedUrl: embed, Title: title, Description: description,
			ScheduledAt: scheduled, ID: e.ID,
		})
		msg = "Stream saved."
	case "start":
		err = h.q.GoLive(ctx, e.ID)
		msg = "You're live. Reminders are going out."
	case "end":
		err = h.q.EndLive(ctx, e.ID)
		msg = "Stream ended. Voters on the questions you answered get an email."
	case "cancel":
		err = h.q.CancelLive(ctx, e.ID)
		msg = "Stream cancelled."
	case "delete":
		err = h.q.DeleteLiveEvent(ctx, e.ID)
		msg, back = "Stream deleted.", "/admin/learn/live"
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.serverError(w, "stream "+r.PathValue("action"), err)
		return
	}
	setFlashCookie(w, msg, "success")
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// TikTok connection. The callback URL must be registered with the TikTok
// app exactly: BASE_URL + /admin/learn/tiktok/callback.
const tiktokStateCookie = "tiktok_state"

func tiktokRedirect(r *http.Request) string { return siteURL(r) + "/admin/learn/tiktok/callback" }

// AdminTikTokConnect sends Rob to TikTok to allow the app to read his
// videos.
func (h *Handler) AdminTikTokConnect(w http.ResponseWriter, r *http.Request) {
	tt := h.media.TikTok()
	if tt == nil {
		setFlashCookie(w, "Set TIKTOK_CLIENT_KEY and TIKTOK_CLIENT_SECRET in .env first.", "error")
		http.Redirect(w, r, "/admin/learn", http.StatusSeeOther)
		return
	}
	state, err := auth.RandomHex(16)
	if err != nil {
		h.serverError(w, "tiktok state", err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: tiktokStateCookie, Value: state, Path: "/admin/learn/tiktok",
		MaxAge: 600, HttpOnly: true, Secure: SecurityHeadersProd, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, tt.AuthURL(tiktokRedirect(r), state), http.StatusSeeOther)
}

// AdminTikTokCallback is where TikTok sends Rob back.
func (h *Handler) AdminTikTokCallback(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(tiktokStateCookie)
	http.SetCookie(w, &http.Cookie{Name: tiktokStateCookie, Value: "", Path: "/admin/learn/tiktok", MaxAge: -1})
	state := r.URL.Query().Get("state")
	switch {
	case r.URL.Query().Get("error") != "":
		setFlashCookie(w, "TikTok said: "+r.URL.Query().Get("error_description"), "error")
	case err != nil || state == "" || subtle.ConstantTimeCompare([]byte(c.Value), []byte(state)) != 1:
		setFlashCookie(w, "That TikTok sign-in didn't match. Try Connect TikTok again.", "error")
	default:
		if err := h.media.ConnectTikTok(r.Context(), r.URL.Query().Get("code"), tiktokRedirect(r)); err != nil {
			setFlashCookie(w, "Couldn't connect TikTok: "+err.Error(), "error")
		} else {
			setFlashCookie(w, "TikTok connected. Rob's videos are syncing now.", "success")
		}
	}
	http.Redirect(w, r, "/admin/learn", http.StatusSeeOther)
}

// AdminTikTokDisconnect forgets the TikTok tokens. Synced videos stay.
func (h *Handler) AdminTikTokDisconnect(w http.ResponseWriter, r *http.Request) {
	if err := h.q.DeleteOAuthToken(r.Context(), "tiktok"); err != nil {
		h.serverError(w, "disconnect tiktok", err)
		return
	}
	setFlashCookie(w, "TikTok disconnected. Its videos stay in the library.", "success")
	http.Redirect(w, r, "/admin/learn", http.StatusSeeOther)
}
