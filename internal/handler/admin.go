package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/market"
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
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
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

func (h *Handler) AdminSeedFakePrices(w http.ResponseWriter, r *http.Request) {
	if h.production {
		http.Error(w, "Not available in production", http.StatusForbidden)
		return
	}
	count := market.SeedFakePrices(r.Context(), h.q)
	setFlashCookie(w, fmt.Sprintf("Seeded fake prices for %d stocks (30 days)", count), "success")
	http.Redirect(w, r, "/admin/prices", http.StatusSeeOther)
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
	fetcher := market.NewFetcher(h.q)
	go func() {
		if err := fetcher.FetchAll(h.q); err != nil {
			slog.Error("manual price fetch", "error", err)
		}
	}()
	setFlashCookie(w, "Price fetch started in background", "success")
	http.Redirect(w, r, "/admin/prices", http.StatusSeeOther)
}
