package handler

import (
	"net/http"
	"time"

	"github.com/exploded/game/internal/auth"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Routes is the whole site: the middleware every request goes through, then
// every route. cmd/server serves it; tests build it too, so checks such as
// "every /admin route refuses a player" cover routes added later.
func (h *Handler) Routes(static *Static, csrfSecret []byte, throttle *Throttle) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	// Answer HEAD on every GET route. Link checkers (Outlook's scanner,
	// Microsoft's crawlers) send HEAD, and a 405 reads as a dead link. Safe
	// because no GET signs anyone in or uses up a link (see LoginVerifyPage).
	r.Use(middleware.GetHead)
	r.Use(RequestLogger)
	r.Use(middleware.Compress(5))
	r.Use(SecurityHeaders)
	r.Use(throttle.Middleware)
	r.Use(MaxBodySize(1 << 20)) // 1 MB max request body

	// Rate limiters for public/chat endpoints.
	contactLimiter := NewRateLimiter(5, time.Minute)
	chatLimiter := NewRateLimiter(30, time.Minute)

	// Static assets (directory listing disabled).
	r.Handle("/static/*", static)

	// Serve favicon.ico from root (browsers request /favicon.ico automatically).
	r.Get("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		static.ServeFile(w, r, "favicon.ico")
	})

	// Health check for the uptime monitor, outside the auth groups.
	r.Get("/health", h.Health)

	// The "Rob is live" banner, which every open page checks once a minute
	// (app.js). Public and outside the session groups, so a check costs no
	// session lookup.
	r.Get("/live/banner", h.LiveBanner)

	// For search engines.
	r.Get("/robots.txt", h.Robots)
	r.Get("/sitemap.xml", h.Sitemap)
	r.Get("/"+IndexNowKey+".txt", h.IndexNowKeyFile)

	// Public routes.
	// Sign-in. These POSTs come before there's a session, so the CSRF
	// middleware can't cover them; CrossOriginProtection stops another site
	// submitting them, which would sign a visitor in to the attacker's account.
	loginLimiter := NewRateLimiter(10, time.Minute)
	crossOrigin := http.NewCrossOriginProtection()
	r.Get("/login", h.LoginPage)
	r.Get("/login/check", h.LoginCheck)
	r.Get("/login/verify", h.LoginVerifyPage)
	r.Group(func(r chi.Router) {
		r.Use(crossOrigin.Handler, loginLimiter.Middleware)
		r.Post("/login", h.LoginSubmit)
		r.Post("/login/code", h.LoginCode)
		r.Post("/login/verify", h.LoginVerify)
	})
	// Logout is in the nav on every page, including public ones that have no
	// CSRF token, so it gets the same cross-origin check as sign-in.
	r.With(crossOrigin.Handler).Post("/logout", auth.HandleLogout(h.q))

	// A video starting to play, counted for /admin/stats (app.js). It only adds
	// to a count and never looks at who's watching, so it needs no session:
	// the sign-in page's tour counts too. The cross-origin check and the limit
	// stop other sites and scripts padding the counts.
	playLimiter := NewRateLimiter(30, time.Minute)
	r.With(crossOrigin.Handler, playLimiter.Middleware).Post("/stats/play", h.StatsPlay)

	// Unsubscribe links work without signing in. The POST also takes the
	// RFC 8058 one-click request from mail clients, which has no session.
	r.Get("/email/unsubscribe", h.UnsubscribePage)
	r.Post("/email/unsubscribe", h.Unsubscribe)
	// The invite email's "stop invitations" link, for someone with no account.
	r.Get("/invite/{code}/stop", h.InviteStopPage)
	r.Post("/invite/{code}/stop", h.InviteStop)

	// Public pages (with optional auth for / to send a signed-in user to the
	// dashboard).
	r.Group(func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return auth.OptionalAuth(h.q, next)
		})
		r.Get("/", h.Home)
		r.Get("/privacy", h.PrivacyPage)
		r.Get("/terms", h.TermsPage)
		r.Get("/about", h.AboutPage)
		r.Get("/help", h.HelpPage)
	})

	// Old course addresses, and invite links, so a signed-out friend can open
	// one.
	r.Group(func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return auth.OptionalAuth(h.q, next)
		})
		r.Use(auth.CSRFProtect(csrfSecret))
		// Courses lived at /courses until 9 November 2026.
		r.Get("/courses", h.OldCourseURL)
		r.Get("/courses/*", h.OldCourseURL)
		// An emailed invite: signed-out visitors get the sign-up form.
		r.Get("/invite/{code}", h.InviteLanding)
	})

	// Authenticated routes.
	r.Group(func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return auth.RequireAuth(h.q, next)
		})
		r.Use(auth.CSRFProtect(csrfSecret))

		r.Get("/dashboard", h.Dashboard)
		r.Get("/welcome", h.WelcomePage)
		r.Post("/welcome", h.WelcomeSubmit)

		// Contact: signed in only. Everything sent signed out was spam, and
		// the footer link sends a signed-out visitor to sign in first.
		r.Get("/contact", h.ContactPage)
		r.With(contactLimiter.Middleware).Post("/contact", h.ContactSubmit)

		// Calculators (Rob's spreadsheets and our own). Hidden mock-ups: only
		// /tools links to them until Rob signs them off. They work in the browser
		// only, so nothing typed into them reaches the server.
		r.Get("/tools", h.Tools)
		r.Group(func(r chi.Router) {
			r.Use(countCalculatorView) // how often each is opened, never by whom
			r.Get("/tools/mortgage", h.MortgageCalculator)
			r.Get("/tools/super", h.SuperCalculator)
			r.Get("/tools/stop-working", h.StopWorkingCalculator)
			r.Get("/tools/money-lasted", h.MoneyLastedCalculator)
			r.Get("/tools/compound", h.CompoundCalculator)
			r.Get("/tools/fees", h.FeesCalculator)
			r.Get("/tools/fhss", h.FHSSCalculator)
			r.Get("/tools/co-contribution", h.CoContributionCalculator)
			r.Get("/tools/spouse-contribution", h.SpouseContributionCalculator)
		})

		// Games: one per month, created by the scheduler. Admins change
		// their settings under /admin/games.
		r.Get("/games", h.GamesList)
		// A stock in this month's game: the stock chips on a Learn video.
		r.Get("/stocks/{sid}", h.StockInCurrentGame)
		// Everything inside one game. GameContext adds the game bar.
		r.Route("/games/{id}", func(r chi.Router) {
			r.Use(h.GameContext)

			r.Get("/", h.GameDetail)
			r.Get("/rules", h.GameRules)
			r.Post("/join", h.GameJoin)

			// Every trade the player has made
			r.Get("/history", h.TransactionHistory)

			// Stocks & Trading
			r.Get("/stocks", h.StocksBrowse)
			r.Get("/stocks/{sid}", h.StockDetail)
			r.Get("/stocks/{sid}/price", h.StockPrice)
			r.Get("/stocks/{sid}/bars", h.StockBars)
			r.Get("/search", h.StockSearch)
			r.Post("/trade/{sid}", h.TradeExecute)

			// Leaderboard
			r.Get("/leaderboard", h.Leaderboard)

			// Limit Orders (Feature 1)
			r.Get("/orders", h.LimitOrdersList)
			r.Post("/orders/{oid}/cancel", h.LimitOrderCancel)

			// Watchlist (Feature 2)
			r.Post("/watchlist/{sid}", h.WatchlistAdd)
			r.Delete("/watchlist/{sid}", h.WatchlistRemove)

			// Invite a friend by email
			r.Post("/invite", h.InviteSend)

			// Activity Feed (Feature 10)
			r.Get("/activity", h.ActivityFeed)
		})

		// Learn: Rob's videos, live streams, questions and courses. Signed
		// in only since 17 November 2026 (Rob's call); until then anyone
		// could read them.
		r.Get("/learn", h.LearnHome)
		r.Get("/learn/videos", h.LearnVideos)
		r.Get("/learn/topics/{slug}", h.LearnTopic)
		r.Get("/learn/v/{vid}", h.LearnVideo)
		r.Get("/learn/live", h.LearnLive)
		r.Get("/learn/live/{eid}", h.LearnLiveEvent)
		r.Get("/learn/live/{eid}/queue", h.LearnLiveQueue)
		r.Get("/learn/questions", h.QuestionsPage)
		r.Get("/learn/questions/{qid}", h.QuestionPage)
		r.Get("/learn/courses", h.CoursesPage)
		r.Get("/learn/courses/{slug}", h.CoursePage)
		r.Get("/learn/courses/{slug}/{lid}", h.LessonPage)
		// Their pictures and files: nothing outside the signed-in site uses
		// them (no email or link preview does).
		r.Get("/learn/live/{eid}/calendar.ics", h.LearnLiveCalendar)
		r.Get("/learn/lesson-thumb/{lid}", h.LessonThumb)
		r.Get("/learn/course-cover/{cid}", h.CourseCover)
		r.Get("/news-thumb/{nid}", h.NewsThumb)

		// Learn: asking, voting and stream reminders
		r.Post("/learn/questions", h.QuestionCreate)
		r.Post("/learn/questions/{qid}/vote", h.QuestionVote)
		r.Post("/learn/questions/{qid}/delete", h.QuestionDelete)
		r.With(auth.RequireAdmin).Post("/learn/questions/{qid}/answer", h.QuestionAnswer)
		r.With(auth.RequireAdmin).Post("/learn/questions/{qid}/onair", h.QuestionOnAir)
		r.With(auth.RequireAdmin).Post("/learn/questions/{qid}/hide", h.QuestionHide)
		r.Post("/learn/live/{eid}/remind", h.LearnLiveRemind)

		// Courses: ticking off a lesson
		r.Post("/learn/courses/{slug}/{lid}/complete", h.LessonComplete)

		// Lobby Chat
		r.Get("/lobby", h.LobbyPage)
		r.With(chatLimiter.Middleware).Post("/lobby", h.LobbySend)

		// Profile & Avatars
		r.Post("/profile/avatar", h.ProfileUpdateAvatar)

		// Achievements (Feature 8)
		r.Get("/achievements", h.AchievementsPage)

		// Notifications (Feature 19)
		r.Get("/notifications", h.NotificationsPage)
		r.Post("/notifications/{nid}/read", h.NotificationMarkRead)
		r.Post("/notifications/read-all", h.NotificationMarkAllRead)

		// Account Settings
		r.Get("/settings", h.SettingsPage)
		r.Post("/settings/profile", h.SettingsProfile)
		r.Post("/settings/email", h.SettingsEmail)
		r.Post("/settings/delete", h.AccountDelete)
		r.Get("/settings/export", h.ExportPersonalData)

		// Admin
		r.Route("/admin", func(r chi.Router) {
			r.Use(func(next http.Handler) http.Handler {
				return auth.RequireAdmin(next)
			})
			r.Get("/", h.AdminDashboard)
			r.Get("/stats", h.AdminStats)
			r.Get("/games", h.AdminGames)
			r.Get("/games/{gid}", h.AdminGame)
			r.Post("/games/{gid}", h.AdminGameUpdate)
			r.Post("/games/{gid}/cancel", h.AdminGameCancel)
			r.Get("/users", h.AdminUsers)
			r.Post("/users/{id}/admin", h.AdminToggle)
			r.Post("/users/{id}/block", h.AdminBlockToggle)
			r.Get("/prices", h.AdminPrices)
			r.Get("/stocks", h.AdminStocks)
			r.Post("/stocks", h.AdminStockAdd)
			r.Post("/stocks/{sid}/active", h.AdminStockActive)
			r.Post("/stocks/{sid}/listing", h.AdminStockListing)
			r.Post("/stocks/listings/dismiss", h.AdminListingDismiss)
			r.Get("/messages", h.AdminContactMessages)
			r.Get("/email", h.AdminEmail)
			r.Post("/email/optouts", h.AdminInviteOptoutAdd)
			r.Post("/email/optouts/delete", h.AdminInviteOptoutDelete)
			r.Get("/courses", h.AdminCourses)
			r.Post("/courses", h.AdminCourseCreate)
			r.Get("/courses/{cid}", h.AdminCourse)
			r.Post("/courses/{cid}", h.AdminCourseUpdate)
			r.Post("/courses/{cid}/publish", h.AdminCoursePublish)
			r.Post("/courses/{cid}/delete", h.AdminCourseDelete)
			r.Post("/courses/{cid}/lessons", h.AdminLessonCreate)
			r.Post("/courses/{cid}/vimeo", h.AdminCourseVimeo)
			r.Post("/courses/{cid}/cover", h.AdminCourseCover)
			r.Post("/courses/{cid}/cover/delete", h.AdminCourseCoverDelete)
			r.Post("/lessons/{lid}", h.AdminLessonUpdate)
			r.Post("/lessons/{lid}/delete", h.AdminLessonDelete)
			r.Get("/learn", h.AdminLearn)
			r.Post("/learn/sync", h.AdminLearnSync)
			r.Post("/learn/videos", h.AdminLearnPaste)
			r.Get("/learn/videos/{vid}", h.AdminLearnVideo)
			r.Post("/learn/videos/{vid}", h.AdminLearnVideoUpdate)
			r.Post("/learn/videos/{vid}/reclassify", h.AdminLearnVideoReclassify)
			r.Post("/learn/videos/{vid}/delete", h.AdminLearnVideoDelete)
			r.Get("/learn/topics", h.AdminLearnTopics)
			r.Post("/learn/topics", h.AdminLearnTopicCreate)
			r.Post("/learn/topics/{tid}", h.AdminLearnTopicUpdate)
			r.Post("/learn/topics/{tid}/delete", h.AdminLearnTopicDelete)
			r.Get("/learn/live", h.AdminLearnLive)
			r.Post("/learn/live", h.AdminLearnLiveCreate)
			r.Post("/learn/live/{eid}/{action}", h.AdminLearnLiveAction)
			r.Post("/email", h.AdminEmailSend)
			r.Post("/messages/{mid}/read", h.AdminMarkMessageRead)
			r.Post("/messages/{mid}/delete", h.AdminDeleteMessage)
			r.Post("/lobby/{mid}/delete", h.AdminLobbyDelete)
			r.Post("/lobby/{mid}/unblock", h.AdminLobbyUnblock)
			r.Post("/lobby/{mid}/block-user", h.AdminLobbyBlockUser)
		})
	})

	return r
}
