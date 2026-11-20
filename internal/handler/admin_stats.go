package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/stats"
)

// statsDays is how many days /admin/stats lists, today included.
const statsDays = 30

// statsTop is how many videos, lessons, courses and calculators each month's
// lists show.
const statsTop = 10

// statCol is one figure on /admin/stats: its label and the metrics added up
// for it (internal/stats).
type statCol struct {
	Label   string
	Metrics []string
}

// statDayCols are the columns of the Last 30 days table.
var statDayCols = []statCol{
	{"Active players", []string{stats.ActiveDay}},
	{"Sign-ups", []string{stats.Signups}},
	{"Trades", []string{stats.Trades}},
	{"Players trading", []string{stats.Traders}},
	{"Video plays", []string{stats.VideoPlay, stats.LivePlay}},
	{"Lesson plays", []string{stats.LessonPlay}},
	{"Lessons done", []string{stats.LessonsDone}},
	{"Calculators opened", []string{stats.CalculatorView}},
	{"Chat messages", []string{stats.ChatPosted}},
	{"Questions", []string{stats.Questions}},
}

// Rows of the Months table that aren't a sum of days, picked out by these
// labels in statsMonths.
const (
	statActiveMonth = "Active players"
	statActiveAvg   = "Active on an average day"
	statPlayers     = "Players in the month's game"
	statTraded      = "Players who traded"
)

// statMonthRows are the rows of the Months table.
var statMonthRows = []statCol{
	{statActiveMonth, nil},
	{statActiveAvg, nil},
	{"Sign-ups", []string{stats.Signups}},
	{statPlayers, nil},
	{statTraded, nil},
	{"Trades", []string{stats.Trades}},
	{"First trades in a game", []string{stats.FirstTrades}},
	{"Video plays", []string{stats.VideoPlay, stats.LivePlay}},
	{"Lesson plays", []string{stats.LessonPlay}},
	{"Lessons marked done", []string{stats.LessonsDone}},
	{"Course pages opened", []string{stats.CourseView}},
	{"Calculators opened", []string{stats.CalculatorView}},
	{"Tour video plays", []string{stats.IntroPlay}},
	{"Chat messages", []string{stats.ChatPosted}},
	{"Chat messages blocked", []string{stats.ChatBlocked}},
	{"Questions for Rob", []string{stats.Questions}},
	{"Question votes", []string{stats.QuestionVotes}},
	{"Invites sent", []string{stats.Invites}},
	{"Invite bonuses paid", []string{stats.Referrals}},
}

// statCell is one figure in a table: a count, or Text when it isn't one.
type statCell struct {
	N    int64
	Text string
}

type statDay struct {
	Label string
	Today bool
	Cells []statCell
}

type statMonthRow struct {
	Label string
	Cells []statCell
}

// statTopItem is one video, lesson, course or calculator in a month's lists.
type statTopItem struct {
	Title string
	URL   string
	N     int64
}

type statTopGroup struct {
	Name  string
	Items []statTopItem
}

type statMonthTops struct {
	Month  string
	Groups []statTopGroup
}

// AdminStats is /admin/stats: the site's usage counts by month and day, and
// the most used videos, lessons, courses and calculators. Counts only: none
// of it says who did what (internal/stats).
func (h *Handler) AdminStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// Write the counts still in memory, so the page is up to the minute.
	if err := stats.Flush(ctx, h.rawDB); err != nil {
		slog.Error("usage stats: flush", "error", err)
	}
	rows, err := h.q.ListSiteStats(ctx)
	if err != nil {
		h.serverError(w, "list site stats", err)
		return
	}
	counts := map[string]map[string]int64{}
	put := func(period, metric string, n int64) {
		if counts[period] == nil {
			counts[period] = map[string]int64{}
		}
		counts[period][metric] = n
	}
	for _, row := range rows {
		put(row.Period, row.Metric, row.N)
	}

	// Today hasn't been stored from the tables yet, nor yesterday in the
	// minute after midnight: count them now.
	now := stats.Now()
	todayStart := stats.DayStart(now)
	today := todayStart.Format(time.DateOnly)
	yesterday := todayStart.AddDate(0, 0, -1).Format(time.DateOnly)
	live := []string{today}
	if stats.LastRolledDay(ctx, h.q) < yesterday {
		live = append(live, yesterday)
	}
	for _, day := range live {
		c, err := stats.DayCounts(ctx, h.q, day)
		if err != nil {
			h.serverError(w, "count today's stats", err)
			return
		}
		for metric, n := range c {
			put(day, metric, n)
		}
	}

	players, err := h.q.ListMonthlyPlayers(ctx)
	if err != nil {
		h.serverError(w, "list monthly players", err)
		return
	}

	months := statsMonths(counts, players, now)
	thisMonth := stats.MonthOf(now)
	lastMonth := stats.MonthOf(stats.MonthStart(now).AddDate(0, -1, 0))
	var tops []statMonthTops
	for _, m := range []string{thisMonth, lastMonth} {
		t, err := h.statsTops(ctx, m)
		if err != nil {
			h.serverError(w, "list item stats", err)
			return
		}
		tops = append(tops, t)
	}

	h.render(w, r, "admin/stats", "", PageData{
		Title: "Stats",
		Extra: map[string]any{
			"DayCols": statDayCols,
			"Days":    statsLastDays(counts, todayStart),
			"Months":  months.labels,
			"Rows":    months.rows,
			"Tops":    tops,
		},
	})
}

// sum adds up col's metrics in one period's counts.
func (c statCol) sum(period map[string]int64) int64 {
	var n int64
	for _, m := range c.Metrics {
		n += period[m]
	}
	return n
}

// statsLastDays is the Last 30 days table, today first.
func statsLastDays(counts map[string]map[string]int64, todayStart time.Time) []statDay {
	days := make([]statDay, 0, statsDays)
	for i := range statsDays {
		d := todayStart.AddDate(0, 0, -i)
		period := counts[d.Format(time.DateOnly)]
		row := statDay{Label: d.Format("Mon 2 Jan"), Today: i == 0}
		for _, col := range statDayCols {
			row.Cells = append(row.Cells, statCell{N: col.sum(period)})
		}
		days = append(days, row)
	}
	return days
}

type statsMonthTable struct {
	labels []string
	rows   []statMonthRow
}

// statsMonths is the Months table: one column per month, newest first, from
// the first month with any count to this one.
func statsMonths(counts map[string]map[string]int64, players []db.ListMonthlyPlayersRow, now time.Time) statsMonthTable {
	current := stats.MonthStart(now)
	first := current
	for period := range counts {
		if len(period) < 7 {
			continue
		}
		if t, err := time.ParseInLocation("2006-01", period[:7], stats.Location()); err == nil && t.Before(first) {
			first = t
		}
	}
	inGame := map[string]db.ListMonthlyPlayersRow{}
	for _, p := range players {
		inGame[p.Month] = p
	}

	var table statsMonthTable
	var months []time.Time
	for m := current; !m.Before(first); m = m.AddDate(0, -1, 0) {
		months = append(months, m)
		table.labels = append(table.labels, m.Format("Jan 2006"))
	}
	for _, col := range statMonthRows {
		row := statMonthRow{Label: col.Label}
		for _, m := range months {
			key := m.Format("2006-01")
			var cell statCell
			switch col.Label {
			case statActiveMonth:
				cell.N = counts[key][stats.ActiveMonth]
			case statActiveAvg:
				cell.Text = statsAverageActive(counts, m, now)
			case statPlayers:
				cell.N = inGame[key].Players
			case statTraded:
				cell.N = inGame[key].Traded
			default:
				for d := m; d.Before(m.AddDate(0, 1, 0)); d = d.AddDate(0, 0, 1) {
					cell.N += col.sum(counts[d.Format(time.DateOnly)])
				}
			}
			row.Cells = append(row.Cells, cell)
		}
		table.rows = append(table.rows, row)
	}
	return table
}

// statsAverageActive is the average of a month's daily active players, over
// the days that have a count (active players were first counted on 20
// November 2026, so earlier days would only drag it down), or "" if none.
func statsAverageActive(counts map[string]map[string]int64, month, now time.Time) string {
	var total, days int64
	for d := month; d.Before(month.AddDate(0, 1, 0)) && !d.After(now); d = d.AddDate(0, 0, 1) {
		if n := counts[d.Format(time.DateOnly)][stats.ActiveDay]; n > 0 {
			total += n
			days++
		}
	}
	if days == 0 {
		return ""
	}
	return fmt.Sprintf("%.1f", float64(total)/float64(days))
}

// statsTopGroups names the lists shown for each month, in order.
var statsTopGroups = []struct{ metric, name string }{
	{stats.VideoPlay, "Videos played"},
	{stats.LivePlay, "Live streams played"},
	{stats.LessonPlay, "Lessons played"},
	{stats.CourseView, "Course pages opened"},
	{stats.CalculatorView, "Calculators opened"},
}

// statsTops is a month's most played videos and lessons and most opened
// courses and calculators, statsTop of each.
func (h *Handler) statsTops(ctx context.Context, month string) (statMonthTops, error) {
	t, _ := time.ParseInLocation("2006-01", month, stats.Location())
	out := statMonthTops{Month: t.Format("January 2006")}
	rows, err := h.q.ListItemStats(ctx, month)
	if err != nil {
		return out, err
	}
	for _, g := range statsTopGroups {
		group := statTopGroup{Name: g.name}
		for _, row := range rows { // most first
			if row.Metric != g.metric || len(group.Items) == statsTop {
				continue
			}
			title, url := h.statItemTitle(ctx, row.Metric, row.Item)
			group.Items = append(group.Items, statTopItem{Title: title, URL: url, N: row.N})
		}
		if len(group.Items) > 0 {
			out.Groups = append(out.Groups, group)
		}
	}
	return out, nil
}

// statItemTitle names a counted video, stream, lesson, course or calculator,
// with a link to it. Something deleted since shows as its id, unlinked.
func (h *Handler) statItemTitle(ctx context.Context, metric, item string) (title, url string) {
	id, _ := strconv.ParseInt(item, 10, 64)
	gone := "(deleted, #" + item + ")"
	switch metric {
	case stats.VideoPlay:
		if v, err := h.q.GetVideo(ctx, id); err == nil {
			return v.Title, "/learn/v/" + item
		}
	case stats.LivePlay:
		if e, err := h.q.GetLiveEvent(ctx, id); err == nil {
			return streamTitle(e.Title, e.Status), "/learn/live/" + item
		}
	case stats.LessonPlay:
		if l, err := h.q.GetLesson(ctx, id); err == nil {
			if c, err := h.q.GetCourse(ctx, l.CourseID); err == nil {
				return c.Title + ": " + l.Title, "/learn/courses/" + c.Slug + "/" + item
			}
			return l.Title, ""
		}
	case stats.CourseView:
		if c, err := h.q.GetCourse(ctx, id); err == nil {
			return c.Title, "/learn/courses/" + c.Slug
		}
	case stats.CalculatorView:
		for _, c := range calculators {
			if c.Slug == item {
				return c.Title, "/tools/" + item
			}
		}
		return "/tools/" + item, ""
	}
	return gone, ""
}
