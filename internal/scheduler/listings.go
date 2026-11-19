package scheduler

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"sync/atomic"
	"time"

	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/jobs"
	"github.com/exploded/game/internal/mail"
	"github.com/exploded/game/internal/market"
)

var listingsRunning atomic.Bool

// listingsTriedAt is when the last new-listings check started (Unix
// seconds).
var listingsTriedAt atomic.Int64

// listingsDayKey is the media_state row holding the New York date of the
// last good new-listings check. It's kept in the database, not memory, so a
// restart or deploy doesn't run the check again: each run spends a
// 50-credit /statistics call and one of its 15 tries on every pending
// listing.
const listingsDayKey = "listings_day"

// listingsHourUTC is the hour from which the new-listings check runs each
// New York weekday: after the splits check and well before the open, so a
// stock that first traded the day before has its market cap by then.
const listingsHourUTC = 11

// maybeCheckListings looks for new NYSE and Nasdaq listings once each New
// York weekday (market.CheckListings) and emails admins about any at S&P 500
// size. It's off when TWELVEDATA_SYMBOLS is set: a dev server shares the API
// key, and its own list would email its own admins.
func maybeCheckListings(ctx context.Context, q *db.Queries, rawDB *sql.DB, m *mail.Mailer, now time.Time) {
	if market.NewYork == nil || now.UTC().Hour() < listingsHourUTC || os.Getenv("TWELVEDATA_SYMBOLS") != "" {
		return
	}
	ny := now.In(market.NewYork)
	if ny.Weekday() == time.Saturday || ny.Weekday() == time.Sunday {
		return
	}
	today := ny.Format(time.DateOnly)
	if done, _ := q.GetMediaState(ctx, listingsDayKey); done == today {
		return
	}
	if now.Sub(time.Unix(listingsTriedAt.Load(), 0)) < dailyRetry {
		return
	}
	if !listingsRunning.CompareAndSwap(false, true) {
		return
	}
	listingsTriedAt.Store(now.Unix())
	jobs.Go("new listings", func() {
		defer listingsRunning.Store(false)
		found, err := market.NewFetcher(q, rawDB).CheckListings(ctx, q, today)
		if err != nil {
			slog.Error("new listings", "error", err)
		} else if err := q.SetMediaState(ctx, db.SetMediaStateParams{Key: listingsDayKey, Value: today}); err != nil {
			slog.Error("new listings: record day", "error", err)
		}
		if len(found) > 0 {
			tellAdmins(ctx, q, m, found)
		}
	})
}

// newListingEmail is the data for new_listing.tmpl.
type newListingEmail struct {
	Listings []newListingLine
	URL      string // the admin stocks page
}

type newListingLine struct {
	Symbol, Exchange, Name, MarketCap string
}

// tellAdmins emails each admin one message listing the new S&P-size
// listings, and leaves each a notification.
func tellAdmins(ctx context.Context, q *db.Queries, m *mail.Mailer, found []market.NewListing) {
	admins, err := q.ListAdminIDs(ctx)
	if err != nil {
		slog.Error("list admins", "error", err)
		return
	}
	data := newListingEmail{URL: "/admin/stocks"}
	for _, l := range found {
		data.Listings = append(data.Listings, newListingLine{
			Symbol: l.Symbol, Exchange: l.Exchange, Name: l.Name, MarketCap: market.Billions(l.MarketCap),
		})
	}
	for _, id := range admins {
		for _, l := range data.Listings {
			q.CreateNotification(ctx, db.CreateNotificationParams{
				UserID: id, Type: "new_listing",
				Title:   "New listing: " + l.Name + " (" + l.Symbol + ")",
				Message: "Listed on the " + l.Exchange + " at a market cap of " + l.MarketCap + ". Add it at /admin/stocks.",
			})
		}
		if m == nil {
			continue
		}
		// Account, so it can't be turned off: admins asked for this one.
		if _, err := m.ToUser(ctx, id, mail.Account, "new_listing", data); err != nil {
			slog.Error("queue new listing email", "user", id, "error", err)
		}
	}
}
