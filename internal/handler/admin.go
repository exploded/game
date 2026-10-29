package handler

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/market"
	"github.com/exploded/game/internal/scheduler"
)

func (h *Handler) AdminDashboard(w http.ResponseWriter, r *http.Request) {
	users, _ := h.q.ListUsers(r.Context())
	stockCount, _ := h.q.CountStocks(r.Context())
	logs, _ := h.q.ListFetchLogs(r.Context())

	h.render(w, r, "admin/index", "", PageData{
		Title: "Admin",
		Extra: map[string]any{
			"UserCount":  len(users),
			"StockCount": stockCount,
			"FetchLogs":  logs,
		},
	})
}

func (h *Handler) AdminUsers(w http.ResponseWriter, r *http.Request) {
	users, _ := h.q.ListUsers(r.Context())
	h.render(w, r, "admin/users", "", PageData{
		Title: "Users",
		Items: users,
	})
}

func (h *Handler) AdminToggle(w http.ResponseWriter, r *http.Request) {
	currentUser := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Prevent admins from demoting themselves.
	if id == currentUser.ID {
		setFlashCookie(w, "You cannot change your own admin status", "error")
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
		return
	}

	user, err := h.q.GetUser(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	newVal := int64(1)
	if user.IsAdmin == 1 {
		newVal = 0
	}
	_ = h.q.SetAdmin(r.Context(), db.SetAdminParams{IsAdmin: newVal, ID: id})
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (h *Handler) AdminPrices(w http.ResponseWriter, r *http.Request) {
	logs, _ := h.q.ListFetchLogs(r.Context())
	stockCount, _ := h.q.CountStocks(r.Context())

	h.render(w, r, "admin/prices", "", PageData{
		Title: "Price Management",
		Items: logs,
		Extra: map[string]any{
			"StockCount": stockCount,
		},
	})
}

// AdminPriceHealth shows price data gaps and missing data (Feature 21).
func (h *Handler) AdminPriceHealth(w http.ResponseWriter, r *http.Request) {
	today := time.Now().UTC().Format("2006-01-02")
	missingCount, _ := h.q.CountStocksMissingPrices(r.Context(), today)
	missingStocks, _ := h.q.ListStocksMissingPrices(r.Context(), today)
	oldestDate, _ := h.q.GetOldestPriceDate(r.Context())
	stockCount, _ := h.q.CountStocks(r.Context())

	h.render(w, r, "admin/pricehealth", "", PageData{
		Title: "Price Data Health",
		Extra: map[string]any{
			"Today":         today,
			"MissingCount":  missingCount,
			"MissingStocks": missingStocks,
			"OldestDate":    oldestDate,
			"StockCount":    stockCount,
		},
	})
}

// AdminUserActivity shows user engagement metrics (Feature 22).
func (h *Handler) AdminUserActivity(w http.ResponseWriter, r *http.Request) {
	activeUsers, _ := h.q.ListMostActiveUsers(r.Context())
	userCount := len(activeUsers)

	h.render(w, r, "admin/activity", "", PageData{
		Title: "User Activity",
		Items: activeUsers,
		Extra: map[string]any{
			"UserCount": userCount,
		},
	})
}

func (h *Handler) AdminFetchPrices(w http.ResponseWriter, r *http.Request) {
	fetcher := market.NewFetcher(h.q, h.rawDB)
	go func() {
		if err := fetcher.FetchAll(h.q); err != nil {
			slog.Error("manual price fetch", "error", err)
		}
		scheduler.SignalPricesChanged()
	}()
	setFlashCookie(w, "Price fetch started in background", "success")
	http.Redirect(w, r, "/admin/prices", http.StatusSeeOther)
}

// AdminFetchNews triggers a TickerTick news refresh for S&P 500 stocks. Runs
// in the background — takes ~90 min due to rate limiting (10 req/min).
func (h *Handler) AdminFetchNews(w http.ResponseWriter, r *http.Request) {
	fetcher := market.NewFetcher(h.q, h.rawDB)
	go func() {
		ctx := context.Background()
		if err := fetcher.FetchTickerTickNews(ctx, h.q, market.TickerTickMarket); err != nil {
			slog.Error("manual tickertick news refresh", "error", err)
		}
	}()
	setFlashCookie(w, "News refresh started in background (~90 min)", "success")
	http.Redirect(w, r, "/admin/prices", http.StatusSeeOther)
}

func (h *Handler) AdminContactMessages(w http.ResponseWriter, r *http.Request) {
	msgs, _ := h.q.ListContactMessages(r.Context(), db.ListContactMessagesParams{
		Limit: 100, Offset: 0,
	})
	h.render(w, r, "admin/messages", "", PageData{
		Title: "Contact Messages",
		Items: msgs,
	})
}

func (h *Handler) AdminMarkMessageRead(w http.ResponseWriter, r *http.Request) {
	mid, err := strconv.ParseInt(r.PathValue("mid"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_ = h.q.MarkContactMessageRead(r.Context(), mid)
	http.Redirect(w, r, "/admin/messages", http.StatusSeeOther)
}
