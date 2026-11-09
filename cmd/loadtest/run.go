package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	mrand "math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// A page a player opens, and how often, out of every 100 requests. The mix
// follows a phone visit: home, the game, the stocks list, a couple of stock
// pages left open (each polls its price every minute), a trade on about one
// visit in five, the ranks and some of Learn.
type page struct {
	name   string
	weight int
	htmx   bool
	post   bool
	path   func(p *Players, r *mrand.Rand) string
}

var chips = []string{"picks", "all", "mag7", "tech", "up", "down", "big", "active", "intl", "crypto", "popular", "watch"}

var pages = []page{
	{name: "dashboard", weight: 10, path: func(p *Players, r *mrand.Rand) string { return "/dashboard" }},
	{name: "game home", weight: 12, path: func(p *Players, r *mrand.Rand) string { return fmt.Sprintf("/games/%d", p.GameID) }},
	{name: "stocks list", weight: 8, path: func(p *Players, r *mrand.Rand) string {
		return fmt.Sprintf("/games/%d/stocks?f=%s", p.GameID, chips[r.IntN(len(chips))])
	}},
	{name: "stocks chip (htmx)", weight: 5, htmx: true, path: func(p *Players, r *mrand.Rand) string {
		return fmt.Sprintf("/games/%d/stocks?f=%s", p.GameID, chips[r.IntN(len(chips))])
	}},
	{name: "stock page", weight: 18, path: func(p *Players, r *mrand.Rand) string {
		return fmt.Sprintf("/games/%d/stocks/%d", p.GameID, p.Stocks[r.IntN(len(p.Stocks))])
	}},
	{name: "price poll (htmx)", weight: 15, htmx: true, path: func(p *Players, r *mrand.Rand) string {
		return fmt.Sprintf("/games/%d/stocks/%d/price", p.GameID, p.Stocks[r.IntN(len(p.Stocks))])
	}},
	{name: "candles (wide screens)", weight: 4, path: func(p *Players, r *mrand.Rand) string {
		return fmt.Sprintf("/games/%d/stocks/%d/bars", p.GameID, p.Stocks[r.IntN(len(p.Stocks))])
	}},
	{name: "search (htmx)", weight: 2, htmx: true, path: func(p *Players, r *mrand.Rand) string {
		return fmt.Sprintf("/games/%d/search?q=%s", p.GameID, []string{"a", "ap", "mi", "nv", "ba", "co"}[r.IntN(6)])
	}},
	{name: "leaderboard", weight: 6, path: func(p *Players, r *mrand.Rand) string { return fmt.Sprintf("/games/%d/leaderboard", p.GameID) }},
	{name: "activity", weight: 3, path: func(p *Players, r *mrand.Rand) string { return fmt.Sprintf("/games/%d/activity", p.GameID) }},
	{name: "lobby", weight: 3, path: func(p *Players, r *mrand.Rand) string { return "/lobby" }},
	{name: "learn", weight: 3, path: func(p *Players, r *mrand.Rand) string { return "/learn" }},
	{name: "video", weight: 5, path: func(p *Players, r *mrand.Rand) string {
		if len(p.Videos) == 0 {
			return "/learn"
		}
		return fmt.Sprintf("/learn/v/%d", p.Videos[r.IntN(len(p.Videos))])
	}},
	{name: "questions", weight: 3, path: func(p *Players, r *mrand.Rand) string { return "/learn/questions" }},
	{name: "trade (htmx POST)", weight: 3, htmx: true, post: true, path: func(p *Players, r *mrand.Rand) string {
		return fmt.Sprintf("/games/%d/trade/%d", p.GameID, p.Stocks[r.IntN(len(p.Stocks))])
	}},
}

type result struct {
	page   string
	status int // 0 for a client error (timeout, refused connection)
	dur    time.Duration
}

type stepStats struct {
	rate                 int
	sent, ok, errs, busy int
	dropped              int64
	p50, p95, p99, max   time.Duration
	healthMax            time.Duration
	healthFail           int
	byPage               map[string][]time.Duration
}

func runCmd(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	base := fs.String("base", "http://127.0.0.1:8999", "test server (never production)")
	file := fs.String("players", "players.json", "sessions written by seed")
	rates := fs.String("rates", "5,10,20,40,80,160", "requests a second for each step")
	stepDur := fs.Duration("step", 60*time.Second, "how long each step runs")
	stopP95 := fs.Duration("stop-p95", 2*time.Second, "stop after a step whose p95 is over this")
	maxInflight := fs.Int64("max-inflight", 3000, "requests the driver will wait on at once; more are dropped and counted")
	out := fs.String("out", "", "also write every step's numbers here as JSON")
	fs.Parse(args)

	if u, err := url.Parse(*base); err != nil || strings.Contains(u.Host, "investorproject.au") || strings.Contains(u.Host, "thewealthlab.au") {
		return fmt.Errorf("-base %q: run against a test server, never production", *base)
	}
	var p Players
	b, err := os.ReadFile(*file)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	if len(p.Players) == 0 || len(p.Stocks) == 0 {
		return fmt.Errorf("%s has no players or stocks", *file)
	}

	client := &http.Client{
		Transport: &http.Transport{MaxIdleConns: 4000, MaxIdleConnsPerHost: 4000, IdleConnTimeout: 90 * time.Second},
		Timeout:   30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse // a redirect is an answer
		},
	}
	total := 0
	for _, pg := range pages {
		total += pg.weight
	}

	// Warm up: fill the stocks cache and the connections.
	fmt.Println("warming up...")
	runStep(client, *base, &p, total, 2, 10*time.Second, *maxInflight)

	var all []stepStats
	for _, s := range strings.Split(*rates, ",") {
		rate, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || rate <= 0 {
			return fmt.Errorf("bad rate %q", s)
		}
		fmt.Printf("%d requests a second for %s...\n", rate, *stepDur)
		st := runStep(client, *base, &p, total, rate, *stepDur, *maxInflight)
		all = append(all, st)
		printStep(st)
		if st.p95 > *stopP95 || float64(st.errs) > 0.01*float64(st.sent) {
			fmt.Println("stopping: past the limit")
			break
		}
	}
	printPages(all, 50)
	printPages(all, 95)
	if *out != "" {
		type row struct {
			Rate                       int
			Sent, OK, Errors, Busy     int
			Dropped                    int64
			P50ms, P95ms, P99ms, MaxMs float64
			HealthMaxMs                float64
			HealthFailures             int
			PageP95ms                  map[string]float64
		}
		var rows []row
		for _, st := range all {
			r := row{Rate: st.rate, Sent: st.sent, OK: st.ok, Errors: st.errs, Busy: st.busy, Dropped: st.dropped,
				P50ms: ms(st.p50), P95ms: ms(st.p95), P99ms: ms(st.p99), MaxMs: ms(st.max),
				HealthMaxMs: ms(st.healthMax), HealthFailures: st.healthFail, PageP95ms: map[string]float64{}}
			for name, d := range st.byPage {
				r.PageP95ms[name] = ms(pct(d, 95))
			}
			rows = append(rows, r)
		}
		b, _ := json.MarshalIndent(rows, "", "  ")
		if err := os.WriteFile(*out, b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// runStep starts rate requests a second for d, whether or not earlier ones
// have finished (an open model: a slow server can't slow the arrivals), and
// samples /health every second.
func runStep(client *http.Client, base string, p *Players, total, rate int, d time.Duration, maxInflight int64) stepStats {
	var (
		mu       sync.Mutex
		results  []result
		wg       sync.WaitGroup
		inflight atomic.Int64
		dropped  atomic.Int64
	)
	r := mrand.New(mrand.NewPCG(uint64(rate), uint64(time.Now().UnixNano())))
	stop := time.Now().Add(d)

	var health []result
	healthDone := make(chan struct{})
	go func() {
		defer close(healthDone)
		hc := &http.Client{Timeout: 5 * time.Second}
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for time.Now().Before(stop) {
			<-t.C
			start := time.Now()
			status := 0
			if resp, err := hc.Get(base + "/health"); err == nil {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				status = resp.StatusCode
			}
			health = append(health, result{status: status, dur: time.Since(start)})
		}
	}()

	tick := time.NewTicker(time.Second / time.Duration(rate))
	defer tick.Stop()
	for time.Now().Before(stop) {
		<-tick.C
		if inflight.Load() >= maxInflight {
			dropped.Add(1)
			continue
		}
		pg := pickPage(r, total)
		pl := p.Players[r.IntN(len(p.Players))]
		path := pg.path(p, r)
		qty := 1 + r.IntN(5)
		inflight.Add(1)
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer inflight.Add(-1)
			res := do(client, base, pg, pl, path, qty)
			mu.Lock()
			results = append(results, res)
			mu.Unlock()
		}()
	}
	wg.Wait()
	<-healthDone

	st := stepStats{rate: rate, sent: len(results), dropped: dropped.Load(), byPage: map[string][]time.Duration{}}
	var durs []time.Duration
	for _, res := range results {
		switch {
		case res.status == http.StatusServiceUnavailable:
			st.busy++ // refused by the throttle: quick, and counted apart from errors
		case res.status == 0 || res.status >= 400:
			st.errs++
		default:
			st.ok++
			durs = append(durs, res.dur)
			st.byPage[res.page] = append(st.byPage[res.page], res.dur)
		}
	}
	st.p50, st.p95, st.p99 = pct(durs, 50), pct(durs, 95), pct(durs, 100-1)
	st.max = pct(durs, 100)
	for _, h := range health {
		st.healthMax = max(st.healthMax, h.dur)
		if h.status != http.StatusOK {
			st.healthFail++
		}
	}
	return st
}

func pickPage(r *mrand.Rand, total int) page {
	n := r.IntN(total)
	for _, pg := range pages {
		if n < pg.weight {
			return pg
		}
		n -= pg.weight
	}
	return pages[0]
}

func do(client *http.Client, base string, pg page, pl Player, path string, qty int) result {
	var req *http.Request
	if pg.post {
		form := url.Values{"side": {"buy"}, "quantity": {strconv.Itoa(qty)}}
		req, _ = http.NewRequest(http.MethodPost, base+path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-CSRF-Token", pl.CSRF)
	} else {
		req, _ = http.NewRequest(http.MethodGet, base+path, nil)
	}
	req.AddCookie(&http.Cookie{Name: "session", Value: pl.Session})
	if pg.htmx {
		req.Header.Set("HX-Request", "true")
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return result{page: pg.name, dur: time.Since(start)}
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return result{page: pg.name, status: resp.StatusCode, dur: time.Since(start)}
}

// pct is the p-th percentile; 100 is the slowest.
func pct(d []time.Duration, p int) time.Duration {
	if len(d) == 0 {
		return 0
	}
	s := slices.Clone(d)
	slices.Sort(s)
	i := len(s) * p / 100
	if i >= len(s) {
		i = len(s) - 1
	}
	return s[i]
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func printStep(st stepStats) {
	fmt.Printf("  sent %d, ok %d, errors %d, busy (503) %d, dropped %d | p50 %s p95 %s p99 %s max %s | /health worst %s, failed %d\n",
		st.sent, st.ok, st.errs, st.busy, st.dropped, round(st.p50), round(st.p95), round(st.p99), round(st.max), round(st.healthMax), st.healthFail)
}

// printPages shows each page's p-th percentile at each step.
func printPages(all []stepStats, p int) {
	fmt.Printf("\np%d by page (ms)\n%-24s", p, "page")
	for _, st := range all {
		fmt.Printf("%9s", strconv.Itoa(st.rate)+"/s")
	}
	fmt.Println()
	for _, pg := range pages {
		fmt.Printf("%-24s", pg.name)
		for _, st := range all {
			fmt.Printf("%9.1f", ms(pct(st.byPage[pg.name], p)))
		}
		fmt.Println()
	}
}
