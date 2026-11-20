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
	"github.com/exploded/game/internal/backup"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/handler"
	"github.com/exploded/game/internal/jobs"
	"github.com/exploded/game/internal/mail"
	"github.com/exploded/game/internal/market"
	"github.com/exploded/game/internal/media"
	"github.com/exploded/game/internal/moderate"
	"github.com/exploded/game/internal/scheduler"
	"github.com/exploded/game/internal/stats"
	"github.com/exploded/game/internal/vimeo"
	"github.com/exploded/monitor/pkg/logship"
)

// Capacity limits, set from load tests (docs/capacity.md). Every request
// waits for the one SQLite connection, so working on more than a few at once
// only lengthens the queue. Past the backlog, or after waiting backlogWait
// for a place, a request gets a quick 503 instead of a page that never loads.
const (
	maxInFlight     = 8                // requests worked on at once
	maxBacklog      = 64               // more may wait for a place...
	backlogWait     = 3 * time.Second  // ...for this long
	requestDeadline = 50 * time.Second // a request gives up its place; under WriteTimeout
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
	stats.SetLocation(loc) // usage counts go by days in TIMEZONE

	// Open database.
	rawDB, err := handler.OpenDB("game.db", game.Schema)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer rawDB.Close()
	queries := db.New(rawDB)

	// Seed stocks, achievements, and avatars on startup.
	market.SeedStocks(context.Background(), queries)
	if err := handler.RunSeedMigrations(rawDB); err != nil {
		log.Fatalf("database: %v", err)
	}
	if err := market.LoadListings(context.Background(), queries); err != nil {
		log.Fatalf("database: %v", err)
	}
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
		BaseURL:    os.Getenv("BASE_URL"),
		ReplyTo:    os.Getenv("MAIL_REPLY_TO"),
		NewsSender: os.Getenv("NEWS_SENDER"),
	})
	if err != nil {
		log.Fatalf("email templates: %v", err)
	}
	h.SetMailer(mailer)
	jobs.Go("email worker", func() { mailer.Run(ctx) })

	// Learn: syncs the YouTube channel, watches for Rob live on TikTok and
	// sorts videos into topics.
	// Each part is off until its keys are in .env.
	med := media.New(queries, mailer, loc)
	h.SetMedia(med)

	// Admins fill courses from Consortium's Vimeo folders. Off without
	// VIMEO_TOKEN (a read-only personal access token).
	h.SetVimeo(vimeo.FromEnv())

	// Lobby messages are checked against the chat rules before they're
	// posted, and Claude picks the stock news worth showing. Without the key
	// messages go up unchecked and every story from the allowed sites shows.
	var news market.NewsFilter
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		h.SetModerator(moderate.NewClaude())
		news = moderate.NewNews()
	} else {
		slog.Info("lobby moderation and news filter off: set ANTHROPIC_API_KEY")
	}

	// Daily database backup to S3, through its own IAM user that can only
	// add objects (scripts/s3-backup-setup.sh). Off until the keys are set.
	region := os.Getenv("BACKUP_S3_REGION")
	if region == "" {
		region = "ap-southeast-2"
	}
	bk := backup.New(region, os.Getenv("BACKUP_S3_BUCKET"),
		os.Getenv("BACKUP_AWS_ACCESS_KEY_ID"), os.Getenv("BACKUP_AWS_SECRET_ACCESS_KEY"), "game.db")
	if bk != nil {
		slog.Info("daily database backup on", "bucket", bk.Bucket(), "region", region)
	} else if production {
		slog.Warn("database backup isn't configured; set BACKUP_S3_BUCKET, BACKUP_AWS_ACCESS_KEY_ID and BACKUP_AWS_SECRET_ACCESS_KEY")
	}

	// Load tests (cmd/loadtest, docs/capacity.md) run an offline copy that
	// never fetches quotes, so it would never revalue. This makes it revalue
	// as often as the US session does. Never in production.
	if os.Getenv("LOADTEST_REVALUE") == "1" {
		if scheduler.EnableLoadTestRevalue(production) {
			slog.Warn("load test: revaluing portfolios every quote interval")
		} else {
			slog.Error("LOADTEST_REVALUE is ignored in production")
		}
	}

	// Start background scheduler. /health reports down if its loop stops.
	jobs.Go("scheduler", func() { scheduler.Start(ctx, queries, rawDB, loc, mailer, med, bk, news) })
	h.SetHeartbeat(scheduler.Heartbeat)

	// The CSRF secret is kept in the database: a new one on every start would
	// make every page left open across a restart or deploy fail its next POST.
	csrfSecret, err := auth.LoadCSRFSecret(rawDB)
	if err != nil {
		log.Fatalf("csrf secret: %v", err)
	}

	handler.SecurityHeadersProd = production

	r := h.Routes(static, csrfSecret, handler.NewThrottle(maxInFlight, maxBacklog, backlogWait, requestDeadline))

	srv := &http.Server{
		Addr:              net.JoinHostPort(bind, port),
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second, // the personal data export can take a while
		IdleTimeout:       120 * time.Second,
	}

	// Graceful shutdown on SIGINT/SIGTERM: requests in flight get up to
	// shutdownRequests to finish, then the scheduler, email worker and
	// other background jobs (all stopped by ctx) get what's left of
	// shutdownTotal. main waits for both, so the database isn't closed
	// under them.
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		start := time.Now()
		slog.Info("shutting down server")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownRequests)
		defer shutdownCancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("server shutdown", "error", err)
		}
		if !jobs.Wait(max(shutdownTotal-time.Since(start), 2*time.Second)) {
			slog.Warn("background jobs still running at shutdown")
		}
		// The last minute's usage counts, which the scheduler hasn't written.
		if err := stats.Flush(context.Background(), rawDB); err != nil {
			slog.Error("usage stats: flush at shutdown", "error", err)
		}
		slog.Info("server stopped")
	}()

	slog.Info("server starting", "addr", srv.Addr, "production", production)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
	// ListenAndServe returns as soon as Shutdown starts: wait for it.
	<-shutdownDone
}

// Shutdown limits: how long requests in flight get to finish, and how long
// the whole shutdown may take, background jobs included.
const (
	shutdownRequests = 10 * time.Second
	shutdownTotal    = 15 * time.Second
)

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
