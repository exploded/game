package main

import (
	"bufio"
	"context"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/exploded/game"
	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/handler"
	"github.com/exploded/game/internal/mail"
	"github.com/exploded/game/internal/market"
	"github.com/exploded/game/internal/media"
	"github.com/exploded/game/internal/scheduler"
	"github.com/exploded/monitor/pkg/logship"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	loadEnv(".env")

	// Set up log shipping to monitor portal.
	monitorURL := os.Getenv("MONITOR_URL")
	monitorKey := os.Getenv("MONITOR_API_KEY")

	if monitorURL != "" && monitorKey != "" {
		ship := logship.New(logship.Options{
			Endpoint: monitorURL + "/api/logs",
			APIKey:   monitorKey,
			App:      "game",
			Level:    slog.LevelWarn,
		})
		defer ship.Shutdown()

		logger := slog.New(logship.Multi(
			slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}),
			ship,
		))
		slog.SetDefault(logger)
		slog.Warn("game app started, log shipping active", "endpoint", monitorURL+"/api/logs")
	} else {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8888"
	}
	production := os.Getenv("PROD") == "1"

	// Production only needs to be reachable by Caddy on the same box. Dev
	// listens on every interface so a phone on the LAN can reach it.
	bind := os.Getenv("BIND_ADDR")
	if bind == "" && production {
		bind = "127.0.0.1"
	}

	// Stopped by SIGINT/SIGTERM: shuts down the HTTP server and the scheduler.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Load timezone for game date comparisons.
	tz := os.Getenv("TIMEZONE")
	if tz == "" {
		tz = "UTC"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		log.Fatalf("invalid TIMEZONE %q: %v", tz, err)
	}

	// Open database.
	rawDB, err := handler.OpenDB("game.db", game.Schema)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer rawDB.Close()
	queries := db.New(rawDB)

	// Seed stocks, achievements, and avatars on startup.
	market.SeedStocks(context.Background(), queries)
	handler.SeedAchievements(context.Background(), queries)
	handler.SeedAvatars(context.Background(), queries)

	// Templates and static files are embedded in the binary (see assets.go).
	static, err := handler.NewStatic(game.Static())
	if err != nil {
		log.Fatalf("static: %v", err)
	}
	pages, err := handler.LoadTemplates(game.Templates(), static)
	if err != nil {
		log.Fatalf("templates: %v", err)
	}

	h := handler.New(queries, rawDB, pages, production, loc)

	// Email goes through SES. Dev without credentials prints it to the log;
	// production without them keeps it queued rather than logging sign-in
	// links.
	var sender mail.Sender = mail.Log{}
	if key, secret, from := os.Getenv("SES_ACCESS_KEY_ID"), os.Getenv("SES_SECRET_ACCESS_KEY"), os.Getenv("MAIL_FROM"); key != "" && secret != "" && from != "" {
		region := os.Getenv("SES_REGION")
		if region == "" {
			region = "ap-southeast-2"
		}
		sender = mail.NewSES(region, key, secret, from)
	} else if production {
		slog.Error("email isn't configured; set SES_ACCESS_KEY_ID, SES_SECRET_ACCESS_KEY and MAIL_FROM")
		sender = mail.Unavailable{}
	}
	mailer, err := mail.New(queries, sender, mail.Config{
		BaseURL:     os.Getenv("BASE_URL"),
		NewsReplyTo: os.Getenv("NEWS_REPLY_TO"),
		NewsSender:  os.Getenv("NEWS_SENDER"),
	})
	if err != nil {
		log.Fatalf("email templates: %v", err)
	}
	h.SetMailer(mailer)
	go mailer.Run(ctx)

	// Learn: syncs Rob's YouTube and TikTok and sorts videos into topics.
	// Each part is off until its keys are in .env.
	med := media.New(queries, mailer, loc)
	h.SetMedia(med)

	// Start background scheduler.
	go scheduler.Start(ctx, queries, rawDB, loc, mailer, med)

	// Generate CSRF secret.
	csrfSecret, err := auth.GenerateCSRFSecret()
	if err != nil {
		log.Fatalf("csrf secret: %v", err)
	}

	handler.SecurityHeadersProd = production

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(handler.RequestLogger)
	r.Use(middleware.Compress(5))
	r.Use(handler.SecurityHeaders)
	r.Use(handler.MaxBodySize(1 << 20)) // 1 MB max request body

	// Rate limiters for public/chat endpoints.
	contactLimiter := handler.NewRateLimiter(5, time.Minute)
	chatLimiter := handler.NewRateLimiter(30, time.Minute)

	// Static assets (directory listing disabled).
	r.Handle("/static/*", static)

	// Serve favicon.ico from root (browsers request /favicon.ico automatically).
	r.Get("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		static.ServeFile(w, r, "favicon.ico")
	})

	// Health check for the uptime monitor, outside the auth groups.
	r.Get("/health", h.Health)

	// Public routes.
	// Sign-in. These POSTs come before there's a session, so the CSRF
	// middleware can't cover them; CrossOriginProtection stops another site
	// submitting them, which would sign a visitor in to the attacker's account.
	loginLimiter := handler.NewRateLimiter(10, time.Minute)
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
	r.With(auth.CSRFProtect(csrfSecret)).Post("/logout", auth.HandleLogout(queries))

	// Unsubscribe links work without signing in. The POST also takes the
	// RFC 8058 one-click request from mail clients, which has no session.
	r.Get("/email/unsubscribe", h.UnsubscribePage)
	r.Post("/email/unsubscribe", h.Unsubscribe)

	// Public pages (with optional auth for pre-filling contact form).
	r.Group(func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return auth.OptionalAuth(queries, next)
		})
		r.Get("/privacy", h.PrivacyPage)
		r.Get("/terms", h.TermsPage)
		r.Get("/about", h.AboutPage)
		r.Get("/help", h.HelpPage)
		r.Get("/contact", h.ContactPage)
		r.With(contactLimiter.Middleware).Post("/contact", h.ContactSubmit)
	})

	// Learn: Rob's videos, live streams and questions. Anyone can read them;
	// asking, voting and reminders are in the signed-in group below. Invite
	// links are here too, so a signed-out friend can open one.
	r.Group(func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return auth.OptionalAuth(queries, next)
		})
		r.Use(auth.CSRFProtect(csrfSecret))
		r.Get("/learn", h.LearnHome)
		r.Get("/learn/topics/{slug}", h.LearnTopic)
		r.Get("/learn/v/{vid}", h.LearnVideo)
		r.Get("/learn/thumb/{vid}", h.LearnThumb)
		r.Get("/learn/live", h.LearnLive)
		r.Get("/learn/live/{eid}", h.LearnLiveEvent)
		r.Get("/learn/live/{eid}/queue", h.LearnLiveQueue)
		r.Get("/learn/live/{eid}/calendar.ics", h.LearnLiveCalendar)
		r.Get("/learn/questions", h.QuestionsPage)
		r.Get("/learn/questions/{qid}", h.QuestionPage)
		r.Get("/learn/courses", h.LearnCourses)
		// An emailed invite: signed-out visitors get the sign-up form.
		r.Get("/invite/{code}", h.InviteLanding)
		r.Get("/questions", handler.OldQuestionsRedirect)
		r.Get("/questions/{qid}", handler.OldQuestionsRedirect)
	})

	// Authenticated routes.
	r.Group(func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return auth.RequireAuth(queries, next)
		})
		r.Use(auth.CSRFProtect(csrfSecret))

		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/dashboard", http.StatusTemporaryRedirect)
		})
		r.Get("/dashboard", h.Dashboard)
		r.Get("/welcome", h.WelcomePage)
		r.Post("/welcome", h.WelcomeSubmit)

		// Games: one per month, created by the scheduler. Admins change
		// their settings under /admin/games.
		r.Get("/games", h.GamesList)
		r.Get("/history", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/games", http.StatusMovedPermanently)
		})
		// Everything inside one game. GameContext adds the game bar.
		r.Route("/games/{id}", func(r chi.Router) {
			r.Use(h.GameContext)

			r.Get("/", h.GameDetail)
			r.Get("/rules", h.GameRules)
			r.Post("/join", h.GameJoin)

			// Portfolio
			r.Get("/portfolio", h.Portfolio)
			r.Get("/history", h.TransactionHistory)
			r.Post("/visibility", h.ToggleVisibility)

			// Stocks & Trading
			r.Get("/stocks", h.StocksBrowse)
			r.Get("/stocks/{sid}", h.StockDetail)
			r.Get("/stocks/{sid}/price", h.StockPrice)
			r.Get("/search", h.StockSearch)
			r.Get("/trade/{sid}", h.TradeForm)
			r.Post("/trade/{sid}", h.TradeExecute)

			// Leaderboard
			r.Get("/leaderboard", h.Leaderboard)

			// Limit Orders (Feature 1)
			r.Get("/orders", h.LimitOrdersList)
			r.Post("/orders/{sid}", h.LimitOrderCreate)
			r.Post("/orders/{oid}/cancel", h.LimitOrderCancel)

			// Watchlist (Feature 2)
			r.Get("/watchlist", h.WatchlistPage)
			r.Post("/watchlist/{sid}", h.WatchlistAdd)
			r.Delete("/watchlist/{sid}", h.WatchlistRemove)

			// Invite a friend by email
			r.Post("/invite", h.InviteSend)

			// Chat (Feature 7)
			r.Get("/chat", h.ChatPage)
			r.With(chatLimiter.Middleware).Post("/chat", h.ChatSend)

			// Activity Feed (Feature 10)
			r.Get("/activity", h.ActivityFeed)

			// Analytics (Features 9, 12, 13, 14, 15)
			r.Get("/analytics", h.TradeAnalytics)
			r.Get("/sectors", h.SectorBreakdown)
			r.Get("/compare", h.PortfolioComparison)
			r.Get("/benchmark", h.BenchmarkComparison)

			// Export CSV (Feature 23)
			r.Get("/export/transactions", h.ExportTransactionsCSV)
			r.Get("/export/snapshots", h.ExportSnapshotsCSV)
		})

		// Learn: asking, voting and stream reminders
		r.Post("/learn/questions", h.QuestionCreate)
		r.Post("/learn/questions/{qid}/vote", h.QuestionVote)
		r.Post("/learn/questions/{qid}/delete", h.QuestionDelete)
		r.With(auth.RequireAdmin).Post("/learn/questions/{qid}/answer", h.QuestionAnswer)
		r.With(auth.RequireAdmin).Post("/learn/questions/{qid}/onair", h.QuestionOnAir)
		r.With(auth.RequireAdmin).Post("/learn/questions/{qid}/hide", h.QuestionHide)
		r.Post("/learn/live/{eid}/remind", h.LearnLiveRemind)

		// Courses
		r.Get("/courses", h.CoursesPage)
		r.Get("/courses/{slug}", h.CoursePage)
		r.Get("/courses/{slug}/{lid}", h.LessonPage)
		r.Post("/courses/{slug}/{lid}/complete", h.LessonComplete)

		// Lobby Chat
		r.Get("/lobby", h.LobbyPage)
		r.With(chatLimiter.Middleware).Post("/lobby", h.LobbySend)

		// Profile & Avatars
		r.Get("/profile", h.ProfilePage)
		r.Post("/profile/avatar", h.ProfileUpdateAvatar)

		// Achievements (Feature 8)
		r.Get("/achievements", h.AchievementsPage)

		// Notifications (Feature 19)
		r.Get("/notifications", h.NotificationsPage)
		r.Get("/notifications/count", h.NotificationCount)
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
			r.Get("/games", h.AdminGames)
			r.Get("/games/{gid}", h.AdminGame)
			r.Post("/games/{gid}", h.AdminGameUpdate)
			r.Post("/games/{gid}/cancel", h.AdminGameCancel)
			r.Get("/users", h.AdminUsers)
			r.Post("/users/{id}/admin", h.AdminToggle)
			r.Get("/prices", h.AdminPrices)
			r.Post("/prices/fetch", h.AdminFetchPrices)
			r.Post("/news/refresh", h.AdminFetchNews)
			r.Get("/prices/health", h.AdminPriceHealth)
			r.Get("/activity", h.AdminUserActivity)
			r.Post("/dividend", h.AdminDividend)
			r.Post("/stock-split", h.AdminStockSplit)
			r.Get("/messages", h.AdminContactMessages)
			r.Get("/email", h.AdminEmail)
			r.Get("/courses", h.AdminCourses)
			r.Post("/courses", h.AdminCourseCreate)
			r.Get("/courses/{cid}", h.AdminCourse)
			r.Post("/courses/{cid}", h.AdminCourseUpdate)
			r.Post("/courses/{cid}/publish", h.AdminCoursePublish)
			r.Post("/courses/{cid}/delete", h.AdminCourseDelete)
			r.Post("/courses/{cid}/lessons", h.AdminLessonCreate)
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
			r.Get("/learn/tiktok/connect", h.AdminTikTokConnect)
			r.Get("/learn/tiktok/callback", h.AdminTikTokCallback)
			r.Post("/learn/tiktok/disconnect", h.AdminTikTokDisconnect)
			r.Post("/email", h.AdminEmailSend)
			r.Post("/messages/{mid}/read", h.AdminMarkMessageRead)
		})
	})

	srv := &http.Server{
		Addr:              net.JoinHostPort(bind, port),
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second, // CSV exports can take a while
		IdleTimeout:       120 * time.Second,
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	go func() {
		<-ctx.Done()
		slog.Info("shutting down server")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("server shutdown", "error", err)
		}
	}()

	slog.Info("server starting", "addr", srv.Addr, "production", production)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}

// loadEnv reads a .env file and sets environment variables (does not override existing).
func loadEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
}
