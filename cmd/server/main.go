package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/handler"
	"github.com/exploded/game/internal/market"
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
	} else {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8888"
	}
	production := os.Getenv("PROD") == "1"

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
	rawDB, err := handler.OpenDB("game.db", "sql/schema.sql")
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer rawDB.Close()
	queries := db.New(rawDB)

	// Seed stocks, achievements, and avatars on startup.
	market.SeedStocks(context.Background(), queries)
	handler.SeedAchievements(context.Background(), queries)
	handler.SeedAvatars(context.Background(), queries)

	// Load templates.
	pages, err := handler.LoadTemplates("templates")
	if err != nil {
		log.Fatalf("templates: %v", err)
	}

	h := handler.New(queries, rawDB, pages, production, loc)

	// Generate CSRF secret.
	csrfSecret, err := auth.GenerateCSRFSecret()
	if err != nil {
		log.Fatalf("csrf secret: %v", err)
	}

	// Start background scheduler.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go scheduler.Start(ctx, queries, loc)

	// In dev mode, start fake live price feed (ticks every 30 seconds).
	if !production {
		go market.StartFakeLiveFeed(ctx, queries, 30*time.Second)
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
	staticFS := handler.NewNoListFileServer("static")
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(staticFS)))

	// Serve favicon.ico from root (browsers request /favicon.ico automatically).
	r.Get("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "static/favicon.ico")
	})

	// Public routes.
	r.Get("/login", func(w http.ResponseWriter, r *http.Request) {
		h.LoginPage(w, r)
	})
	r.Get("/auth/google", auth.HandleLogin)
	r.Get("/auth/google/callback", auth.HandleCallback(queries))
	r.With(auth.CSRFProtect(csrfSecret)).Post("/logout", auth.HandleLogout(queries))

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

		// Games
		r.Get("/games", h.GamesList)
		r.Get("/games/new", h.GameNew)
		r.Post("/games", h.GameCreate)
		r.Get("/games/{id}", h.GameDetail)
		r.Post("/games/{id}/join", h.GameJoin)
		r.Post("/games/{id}/cancel", h.GameCancel)

		// Portfolio
		r.Get("/games/{id}/portfolio", h.Portfolio)
		r.Get("/games/{id}/history", h.TransactionHistory)
		r.Post("/games/{id}/visibility", h.ToggleVisibility)

		// Stocks & Trading
		r.Get("/games/{id}/stocks", h.StocksBrowse)
		r.Get("/games/{id}/stocks/{sid}", h.StockDetail)
		r.Get("/games/{id}/search", h.StockSearch)
		r.Get("/games/{id}/trade/{sid}", h.TradeForm)
		r.Post("/games/{id}/trade/{sid}", h.TradeExecute)

		// Leaderboard
		r.Get("/games/{id}/leaderboard", h.Leaderboard)

		// Limit Orders (Feature 1)
		r.Get("/games/{id}/orders", h.LimitOrdersList)
		r.Post("/games/{id}/orders/{sid}", h.LimitOrderCreate)
		r.Post("/games/{id}/orders/{oid}/cancel", h.LimitOrderCancel)

		// Watchlist (Feature 2)
		r.Get("/games/{id}/watchlist", h.WatchlistPage)
		r.Post("/games/{id}/watchlist/{sid}", h.WatchlistAdd)
		r.Delete("/games/{id}/watchlist/{sid}", h.WatchlistRemove)

		// Invite Links (Feature 6)
		r.Post("/games/{id}/invite", h.InviteCreate)
		r.Get("/invite/{code}", h.InviteJoin)

		// Chat (Feature 7)
		r.Get("/games/{id}/chat", h.ChatPage)
		r.With(chatLimiter.Middleware).Post("/games/{id}/chat", h.ChatSend)

		// Lobby Chat
		r.Get("/lobby", h.LobbyPage)
		r.With(chatLimiter.Middleware).Post("/lobby", h.LobbySend)

		// Profile & Avatars
		r.Get("/profile", h.ProfilePage)
		r.Post("/profile/avatar", h.ProfileUpdateAvatar)

		// Activity Feed (Feature 10)
		r.Get("/games/{id}/activity", h.ActivityFeed)

		// Analytics (Features 9, 12, 13, 14, 15)
		r.Get("/games/{id}/analytics", h.TradeAnalytics)
		r.Get("/games/{id}/sectors", h.SectorBreakdown)
		r.Get("/games/{id}/compare", h.PortfolioComparison)
		r.Get("/games/{id}/benchmark", h.BenchmarkComparison)

		// Export CSV (Feature 23)
		r.Get("/games/{id}/export/transactions", h.ExportTransactionsCSV)
		r.Get("/games/{id}/export/snapshots", h.ExportSnapshotsCSV)

		// Achievements (Feature 8)
		r.Get("/achievements", h.AchievementsPage)

		// Notifications (Feature 19)
		r.Get("/notifications", h.NotificationsPage)
		r.Get("/notifications/count", h.NotificationCount)
		r.Post("/notifications/{nid}/read", h.NotificationMarkRead)
		r.Post("/notifications/read-all", h.NotificationMarkAllRead)

		// Game Templates (Feature 16)
		r.Get("/templates", h.GameTemplatesList)
		r.Post("/templates", h.GameTemplateCreate)
		r.Post("/templates/{tid}/create-game", h.GameFromTemplate)

		// Game History (Feature 20)
		r.Get("/history", h.GameHistory)

		// Account Settings
		r.Get("/settings", h.SettingsPage)
		r.Post("/settings/delete", h.AccountDelete)
		r.Get("/settings/export", h.ExportPersonalData)

		// Admin
		r.Route("/admin", func(r chi.Router) {
			r.Use(func(next http.Handler) http.Handler {
				return auth.RequireAdmin(next)
			})
			r.Get("/", h.AdminDashboard)
			r.Get("/users", h.AdminUsers)
			r.Post("/users/{id}/admin", h.AdminToggle)
			r.Get("/prices", h.AdminPrices)
			r.Post("/prices/fetch", h.AdminFetchPrices)
			r.Post("/prices/fake", h.AdminSeedFakePrices)
			r.Get("/prices/health", h.AdminPriceHealth)
			r.Get("/activity", h.AdminUserActivity)
			r.Post("/dividend", h.AdminDividend)
			r.Post("/stock-split", h.AdminStockSplit)
			r.Get("/messages", h.AdminContactMessages)
			r.Post("/messages/{mid}/read", h.AdminMarkMessageRead)
		})
	})

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%s", port),
		Handler:      r,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-quit
		slog.Info("shutting down server")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		srv.Shutdown(shutdownCtx)
	}()

	slog.Info("server starting", "port", port, "production", production)
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
