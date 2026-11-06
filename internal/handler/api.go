package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/market"
	"github.com/go-chi/chi/v5"
)

// The API lets a player's own AI (Claude, ChatGPT, their own agents) see
// their portfolio and trade for them in this month's game. /api/v1 serves
// these functions as JSON to an API key or OAuth token; mcp.go serves the
// same functions as MCP tools. Money is integer cents with the text beside
// it, so a language model doesn't have to do sums to read it. Everything
// comes from the database, so no request costs Twelve Data credits.

// publicURL is the site's address, for links in API answers.
func publicURL() string {
	if base := strings.TrimRight(os.Getenv("BASE_URL"), "/"); base != "" {
		return base
	}
	return "https://thewealthlab.au"
}

// money is an amount in cents, with its text: {"cents": 123456, "text":
// "$1,234.56", "currency": "AUD"}.
type money struct {
	Cents    int64  `json:"cents"`
	Text     string `json:"text"`
	Currency string `json:"currency"`
}

func moneyOf(cents int64, currency string) money {
	return money{Cents: cents, Text: fmtCents(cents), Currency: currency}
}

// move is a gain or loss and its percentage.
type move struct {
	Amount money  `json:"amount"`
	Pct    string `json:"pct,omitempty"`
}

func moveOf(c change, currency string) move {
	return move{Amount: moneyOf(c.Amount, currency), Pct: c.Pct()}
}

// errReadOnly refuses an order from a token that can only read.
var errReadOnly = errors.New("read only")

// apiPlayer is this month's game and the player's place in it, joining them
// to it if they aren't in it yet.
func (h *Handler) apiPlayer(ctx context.Context, userID int64) (db.Game, db.Participant, error) {
	game, ok := h.currentGame(ctx, userID)
	if !ok {
		return db.Game{}, db.Participant{}, refuse("This month's game hasn't been set up yet. Try again in a minute.")
	}
	p, err := h.q.GetParticipantByGameAndUser(ctx, db.GetParticipantByGameAndUserParams{GameID: game.ID, UserID: userID})
	if err != nil {
		return db.Game{}, db.Participant{}, refuse("You couldn't be added to this month's game. It may be full.")
	}
	return game, p, nil
}

// --- Game ---

type apiGame struct {
	ID                int64     `json:"id"`
	Name              string    `json:"name"`
	Status            string    `json:"status"`
	Starts            string    `json:"starts"` // the first day, from midnight in time_zone
	Ends              string    `json:"ends"`   // the last day, to midnight in time_zone
	TimeZone          string    `json:"time_zone"`
	Currency          string    `json:"currency"`
	StartingBalance   money     `json:"starting_balance"`
	TradeFee          money     `json:"trade_fee"`
	CurrencyFee       string    `json:"currency_fee"`
	ShortSelling      bool      `json:"short_selling"`
	LimitOrders       bool      `json:"limit_orders"`
	AfterHoursTrading bool      `json:"after_hours_trading"`
	AITrading         bool      `json:"ai_trading"`
	AIOrdersPerDay    int64     `json:"ai_orders_per_day"` // orders the player's AI may place a day, from midnight in time_zone
	AIOrdersLeft      int64     `json:"ai_orders_left_today"`
	InterestOnCash    string    `json:"interest_on_cash"`
	USMarket          apiMarket `json:"us_market"`
	RulesURL          string    `json:"rules_url"`
	Notes             []string  `json:"notes"`
}

type apiMarket struct {
	Open   bool   `json:"open"`
	Status string `json:"status"` // "Open. Closes in 2 hr 5 min" or "Closed. It opens in ..."
}

func usMarket() apiMarket {
	_, open := market.QuoteStatus()
	now := marketNow()
	if open {
		return apiMarket{Open: true, Status: strings.TrimSpace("Open. " + marketCloses(now))}
	}
	m := apiMarket{Status: "Closed."}
	if o := marketOpens(now); o != "" {
		m.Status += " It opens " + o + "."
	}
	return m
}

func (h *Handler) apiGameInfo(ctx context.Context, user *db.User, game db.Game) apiGame {
	used, _ := h.q.CountAIOrders(ctx, db.CountAIOrdersParams{UserID: user.ID, Day: h.aiDay()})
	g := apiGame{
		ID:                game.ID,
		Name:              game.Name,
		Status:            game.Status,
		Starts:            game.StartDate,
		Ends:              game.EndDate,
		TimeZone:          h.loc.String(),
		Currency:          game.BaseCurrency,
		StartingBalance:   moneyOf(game.StartingBalance, game.BaseCurrency),
		TradeFee:          moneyOf(game.TradeFee, game.BaseCurrency),
		CurrencyFee:       pctBps(game.FxFee) + " of the value of a trade in a stock priced in another currency, on top of the trade fee",
		ShortSelling:      game.AllowShort != 0,
		LimitOrders:       game.LimitOrders != 0,
		AfterHoursTrading: game.AfterHoursTrading != 0,
		AITrading:         game.AiTrading != 0,
		AIOrdersPerDay:    aiOrderLimit(user),
		AIOrdersLeft:      max(aiOrderLimit(user)-used, 0),
		InterestOnCash:    fmt.Sprintf("%.2f%% a year, paid daily", float64(game.CreditInterestRate)/100),
		USMarket:          usMarket(),
		RulesURL:          fmt.Sprintf("%s/games/%d/rules", publicURL(), game.ID),
		Notes: []string{
			"The Wealth Lab is a practice share game with play money. Nothing here is financial advice.",
			"The biggest portfolio (cash plus holdings) when the game ends wins.",
			"Prices are delayed. While the US market is open they're refreshed every few minutes.",
			"Stock splits are applied for you. Dividends are paid in cash on the ex-dividend date to whoever held the stock at that day's open; a short pays the dividend instead.",
		},
	}
	if game.AfterHoursTrading != 0 {
		g.Notes = append(g.Notes, "While the US market is closed, a market order fills straight away at the last close.")
	} else {
		g.Notes = append(g.Notes, "While the US market is closed, a market order waits and fills at the first price after the open.")
	}
	return g
}

// --- Portfolio ---

type apiPortfolio struct {
	Game         string       `json:"game"`
	GameOver     bool         `json:"game_over,omitempty"`
	Cash         money        `json:"cash"`
	Value        money        `json:"value"` // cash plus holdings: the score
	Today        *move        `json:"today,omitempty"`
	TodaySession string       `json:"today_session,omitempty"` // the session today's move is for, when it isn't today's
	SinceStart   move         `json:"since_start"`
	Rank         int          `json:"rank"`   // 0 until the player's first trade
	Ranked       bool         `json:"ranked"` // on the leaderboard: needs a trade
	Players      int          `json:"players"`
	NextUp       string       `json:"next_player_up,omitempty"`
	BehindBy     *money       `json:"behind_by,omitempty"`
	Interest     money        `json:"interest_earned"`
	Dividends    money        `json:"dividends"` // received, less what shorts paid
	Holdings     []apiHolding `json:"holdings"`
}

type apiHolding struct {
	Symbol      string `json:"symbol"`
	Name        string `json:"name"`
	Shares      int64  `json:"shares"` // negative for a short
	AvgPrice    money  `json:"avg_price"`
	Price       money  `json:"price"`
	Value       money  `json:"value"`
	Today       move   `json:"today"`
	Total       move   `json:"total"`
	CloseQueued bool   `json:"close_queued,omitempty"` // an order closing it waits for the open
}

func (h *Handler) apiPortfolio(ctx context.Context, game db.Game, p db.Participant) apiPortfolio {
	pf := h.portfolioFigures(ctx, game, p)
	cur := game.BaseCurrency
	out := apiPortfolio{
		Game:         game.Name,
		GameOver:     pf.Over,
		Cash:         moneyOf(p.CashBalance, cur),
		Value:        moneyOf(p.PortfolioValue, cur),
		TodaySession: sessionDay(pf.Session, marketNow()),
		SinceStart:   moveOf(pf.Total, cur),
		Rank:         pf.Standing.Rank,
		Ranked:       !pf.Standing.Unranked,
		Players:      pf.Standing.Of,
		NextUp:       pf.Standing.Ahead,
		Interest:     moneyOf(pf.Interest, cur),
		Dividends:    moneyOf(pf.Dividends, cur),
		Holdings:     []apiHolding{},
	}
	if pf.Today != nil {
		t := moveOf(*pf.Today, cur)
		out.Today = &t
	}
	if pf.Standing.Ahead != "" {
		gap := moneyOf(pf.Standing.Gap, cur)
		out.BehindBy = &gap
	}
	for _, r := range pf.Rows {
		out.Holdings = append(out.Holdings, apiHolding{
			Symbol:      r.Symbol,
			Name:        r.StockName,
			Shares:      r.Quantity,
			AvgPrice:    moneyOf(r.Avg, r.Currency),
			Price:       moneyOf(r.Price, r.Currency),
			Value:       moneyOf(r.CurrentValue, r.Currency),
			Today:       moveOf(r.Today, r.Currency),
			Total:       moveOf(r.Total, r.Currency),
			CloseQueued: r.CloseQueued,
		})
	}
	return out
}

// --- Stocks ---

type apiStockRow struct {
	Symbol      string `json:"symbol"`
	Name        string `json:"name"`
	Sector      string `json:"sector,omitempty"`
	Market      string `json:"market"`
	Price       money  `json:"price"`
	TodayPct    string `json:"today_pct,omitempty"`
	HealthScore *int   `json:"health_score,omitempty"` // 0 to 5; none for banks, insurers and funds
	Figure      string `json:"figure,omitempty"`       // what a financial filter matched on
	RobsPick    bool   `json:"robs_pick,omitempty"`
	PickNote    string `json:"pick_note,omitempty"`
	SharesHeld  int64  `json:"shares_held,omitempty"`
}

func stockRowOf(r screenRow) apiStockRow {
	out := apiStockRow{
		Symbol:     r.Symbol,
		Name:       r.Name,
		Sector:     r.Sector,
		Market:     market.MarketLabel(r.Symbol, r.Market),
		Price:      moneyOf(r.Price, r.Currency),
		TodayPct:   r.Today.Pct(),
		Figure:     r.Figure,
		RobsPick:   r.IsPick == 1,
		PickNote:   r.PickNote,
		SharesHeld: r.Held,
	}
	if r.HasHealth {
		n := r.Health
		out.HealthScore = &n
	}
	return out
}

type apiStockList struct {
	Caption string        `json:"caption"`
	Filter  string        `json:"filter,omitempty"`
	Explain string        `json:"explain,omitempty"`
	Matches int           `json:"matches"` // before limit
	Stocks  []apiStockRow `json:"stocks"`
	Filters []apiFilter   `json:"filters"`
	Sectors []string      `json:"sectors"`
}

type apiFilter struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// apiListLimit is how many stocks a list returns unless asked for more.
const apiListLimit = 50

func (h *Handler) apiStocks(ctx context.Context, game db.Game, p db.Participant, filter, sector, q string, limit int) (apiStockList, error) {
	rows, err := h.screenRows(ctx, game.Markets)
	if err != nil {
		return apiStockList{}, err
	}
	res := h.screen(ctx, game.ID, p.UserID, rows, filterKey(filter), sector, q)
	h.markRows(ctx, p.ID, p.UserID, res.Rows)
	if limit <= 0 {
		limit = apiListLimit
	}
	out := apiStockList{
		Caption: res.Caption,
		Filter:  res.Filter,
		Explain: res.Explain,
		Matches: len(res.Rows),
		Stocks:  []apiStockRow{},
		Sectors: screenSectors(rows),
	}
	for i, r := range res.Rows {
		if i == limit {
			break
		}
		out.Stocks = append(out.Stocks, stockRowOf(r))
	}
	for _, sf := range screenFilters {
		out.Filters = append(out.Filters, apiFilter{Key: sf.Key, Label: sf.Label})
	}
	return out, nil
}

type apiStock struct {
	apiStockRow
	Industry  string            `json:"industry,omitempty"`
	About     string            `json:"about,omitempty"`
	Latest    *apiBar           `json:"latest,omitempty"`
	Change    string            `json:"change,omitempty"` // against the previous close
	AsOf      string            `json:"as_of,omitempty"`
	Figures   map[string]string `json:"figures,omitempty"`
	Closes    []apiClose        `json:"closes_30_days"`
	Position  *apiPosition      `json:"position,omitempty"`
	StockPage string            `json:"stock_page"`
}

type apiBar struct {
	Date   string `json:"date"`
	Open   money  `json:"open"`
	High   money  `json:"high"`
	Low    money  `json:"low"`
	Close  money  `json:"close"`
	Volume int64  `json:"volume"`
}

type apiClose struct {
	Date  string `json:"date"`
	Close int64  `json:"close_cents"`
}

type apiPosition struct {
	Shares   int64 `json:"shares"`
	AvgPrice money `json:"avg_price"`
	Value    money `json:"value"`
}

// findStock is the stock with this symbol that can be traded in game.
func (h *Handler) findStock(ctx context.Context, game db.Game, symbol string) (screenRow, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return screenRow{}, refuse("Give a stock symbol, such as NVDA")
	}
	rows, err := h.screenRows(ctx, game.Markets)
	if err != nil {
		return screenRow{}, err
	}
	for _, r := range rows {
		if r.Symbol == symbol {
			return r, nil
		}
	}
	return screenRow{}, refuse("%s isn't in the game. Search with find_stocks (GET /api/v1/stocks?q=...) for its symbol.", symbol)
}

// figures are a stock's financials, labelled, leaving out missing ones.
func figures(r screenRow) map[string]string {
	if !hasFinancials(r) {
		return nil
	}
	f := map[string]string{}
	add := func(label string, v int64, text string) {
		if v != 0 {
			f[label] = text
		}
	}
	ratio := func(v int64) string { return fmt.Sprintf("%.2f", float64(v)/100) }
	add("market_value", r.MarketCap, bigMoney(r.MarketCap))
	add("revenue", r.Revenue, bigMoney(r.Revenue))
	add("revenue_growth_year_on_year", r.QuarterlyRevGrowth, growthBps(r.QuarterlyRevGrowth))
	add("net_income", r.NetIncome, bigMoney(r.NetIncome))
	add("earnings_growth_year_on_year", r.QuarterlyEarnGrowth, growthBps(r.QuarterlyEarnGrowth))
	add("profit_margin", r.ProfitMargin, pctBps(r.ProfitMargin))
	add("free_cash_flow", r.FreeCashFlow, bigMoney(r.FreeCashFlow))
	if r.NetIncome > 0 {
		add("pe_ratio", r.TrailingPe, ratio(r.TrailingPe))
		add("peg_ratio", r.PegRatio, ratio(r.PegRatio))
	}
	if balanceSheetFits(r) {
		add("cash", r.TotalCash, bigMoney(r.TotalCash))
		add("debt", r.TotalDebt, bigMoney(r.TotalDebt))
		add("debt_to_equity", r.DebtToEquity, ratio(r.DebtToEquity))
		add("current_ratio", r.CurrentRatio, ratio(r.CurrentRatio))
	}
	return f
}

func (h *Handler) apiStock(ctx context.Context, game db.Game, p db.Participant, symbol string) (apiStock, error) {
	row, err := h.findStock(ctx, game, symbol)
	if err != nil {
		return apiStock{}, err
	}
	rows := []screenRow{row}
	h.markRows(ctx, p.ID, p.UserID, rows)
	row = rows[0]
	out := apiStock{
		apiStockRow: stockRowOf(row),
		Figures:     figures(row),
		Closes:      []apiClose{},
		StockPage:   fmt.Sprintf("%s/games/%d/stocks/%d", publicURL(), game.ID, row.ID),
	}
	if prof, err := h.q.GetStockProfile(ctx, row.ID); err == nil {
		out.Industry = prof.Industry
		out.About = prof.Description
	}
	ps := h.loadPriceStats(ctx, game.ID, row.ID)
	if ps.Latest.ID != 0 {
		l := ps.Latest
		out.Latest = &apiBar{
			Date: l.Date, Open: moneyOf(l.Open, row.Currency), High: moneyOf(l.High, row.Currency),
			Low: moneyOf(l.Low, row.Currency), Close: moneyOf(l.Close, row.Currency), Volume: l.Volume,
		}
		out.Change, out.AsOf = ps.ChangeText, ps.AsOf
	}
	now := time.Now().UTC()
	bars, _ := h.q.ListPriceHistory(ctx, db.ListPriceHistoryParams{
		StockID: row.ID, Date: now.AddDate(0, 0, -45).Format(time.DateOnly), Date_2: now.Format(time.DateOnly),
	})
	if len(bars) > 30 {
		bars = bars[len(bars)-30:]
	}
	for _, b := range bars {
		out.Closes = append(out.Closes, apiClose{Date: b.Date, Close: b.Close})
	}
	if ho, err := h.q.GetHolding(ctx, db.GetHoldingParams{ParticipantID: p.ID, StockID: row.ID}); err == nil && ho.Quantity != 0 {
		out.Position = &apiPosition{
			Shares:   ho.Quantity,
			AvgPrice: moneyOf(market.AvgCost(ho.Quantity, ho.CostBasis), row.Currency),
			Value:    moneyOf(ho.CurrentValue, row.Currency),
		}
	}
	return out, nil
}

// --- Orders ---

type apiOrderRequest struct {
	Symbol     string  `json:"symbol"`
	Side       string  `json:"side"`        // buy | sell
	Quantity   int64   `json:"quantity"`    // whole shares
	Type       string  `json:"type"`        // market (the default) | limit
	LimitPrice float64 `json:"limit_price"` // dollars a share, in the stock's currency
	Expires    string  `json:"expires"`     // YYYY-MM-DD, limit orders only
	CloseAll   bool    `json:"close_all"`   // sell everything held, or buy back a whole short
}

type apiOrderResult struct {
	Preview       bool   `json:"preview,omitempty"`
	Status        string `json:"status"` // filled | queued | open (a limit order waiting for its price)
	Message       string `json:"message"`
	Symbol        string `json:"symbol"`
	Side          string `json:"side"`
	Quantity      int64  `json:"quantity"`
	Price         money  `json:"price"` // a share: the fill, the last close for an estimate, or the limit
	Value         money  `json:"value"`
	Fee           money  `json:"fee"`
	Total         money  `json:"total"` // what it costs (a buy) or brings in (a sell), fees included
	CashAfter     money  `json:"cash_after"`
	Expires       string `json:"expires,omitempty"`
	OrderID       int64  `json:"order_id,omitempty"`
	TransactionID int64  `json:"transaction_id,omitempty"`
}

// apiOrder checks and, unless preview is set, places an order for the
// player's AI. source is "api" or "mcp".
func (h *Handler) apiOrder(ctx context.Context, user *db.User, req apiOrderRequest, source string, preview bool) (apiOrderResult, error) {
	if !preview && !auth.CanTrade(ctx) {
		return apiOrderResult{}, errReadOnly
	}
	game, _, err := h.apiPlayer(ctx, user.ID)
	if err != nil {
		return apiOrderResult{}, err
	}
	row, err := h.findStock(ctx, game, req.Symbol)
	if err != nil {
		return apiOrderResult{}, err
	}
	o := Order{
		UserID:   user.ID,
		GameID:   game.ID,
		StockID:  row.ID,
		Side:     strings.ToLower(strings.TrimSpace(req.Side)),
		Quantity: req.Quantity,
		CloseAll: req.CloseAll,
		Source:   source,
		DryRun:   preview,
	}
	switch strings.ToLower(strings.TrimSpace(req.Type)) {
	case "", "market":
	case "limit":
		o.Limit = true
		if req.LimitPrice > 0 && req.LimitPrice < math.MaxInt64/100 {
			o.LimitPrice = int64(math.Round(req.LimitPrice * 100))
		}
		o.ExpiresAt = strings.TrimSpace(req.Expires)
	default:
		return apiOrderResult{}, refuse("type must be market or limit")
	}
	if o.Side != "buy" && o.Side != "sell" {
		return apiOrderResult{}, refuse("side must be buy or sell")
	}

	release := func() {}
	if !preview {
		var err error
		if release, err = h.reserveAIOrder(ctx, user); err != nil {
			return apiOrderResult{}, err
		}
	}
	res, err := h.PlaceOrder(ctx, o)
	if err != nil {
		release()
		return apiOrderResult{}, err
	}
	out := apiOrderResult{
		Preview:       preview,
		Status:        res.Status,
		Message:       res.Message,
		Symbol:        res.Stock.Symbol,
		Side:          res.Side,
		Quantity:      res.Quantity,
		Price:         moneyOf(res.Price, res.Stock.Currency),
		Value:         moneyOf(res.Value, res.Currency),
		Fee:           moneyOf(res.Fee, res.Currency),
		CashAfter:     moneyOf(res.CashAfter, res.Currency),
		Expires:       res.ExpiresAt,
		OrderID:       res.OrderID,
		TransactionID: res.TransactionID,
	}
	if res.Side == "buy" {
		out.Total = moneyOf(res.Value+res.Fee, res.Currency)
	} else {
		out.Total = moneyOf(max(res.Value-res.Fee, 0), res.Currency)
	}
	if preview {
		out.Message = previewMessage(res, out)
	}
	return out, nil
}

// previewMessage says what an order would do, for the player to confirm.
func previewMessage(res OrderResult, out apiOrderResult) string {
	verb := "Buy"
	if res.Side == "sell" {
		verb = "Sell"
	}
	what := fmt.Sprintf("%s %s of %s", verb, shares(res.Quantity), res.Stock.Symbol)
	var when string
	switch res.Status {
	case orderFilled:
		when = fmt.Sprintf("at the last price, %s a share. It would fill straight away", out.Price.Text)
	case orderQueued:
		when = fmt.Sprintf("at about %s a share (the last close). The US market is closed, so it would wait and fill at the first price after the open", out.Price.Text)
	case orderOpen:
		when = fmt.Sprintf("at %s a share or better. It would wait until the price reaches the limit", out.Price.Text)
	}
	total := "costing"
	if res.Side == "sell" {
		total = "bringing in"
	}
	return fmt.Sprintf("%s %s, %s %s including %s in fees, leaving %s cash. Nothing has been placed yet.",
		what, when, total, out.Total.Text, out.Fee.Text, out.CashAfter.Text)
}

type apiOrderRow struct {
	ID         int64  `json:"id"`
	Symbol     string `json:"symbol"`
	Name       string `json:"name"`
	Side       string `json:"side"`
	Kind       string `json:"kind"` // limit, or market: queued for the open
	Quantity   int64  `json:"quantity"`
	LimitPrice *money `json:"limit_price,omitempty"`
	Expires    string `json:"expires,omitempty"`
	Placed     string `json:"placed"` // UTC
	ViaAI      bool   `json:"via_ai,omitempty"`
}

func (h *Handler) apiOrders(ctx context.Context, p db.Participant) ([]apiOrderRow, error) {
	orders, err := h.q.ListOpenLimitOrders(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	out := []apiOrderRow{}
	for _, o := range orders {
		row := apiOrderRow{
			ID: o.ID, Symbol: o.Symbol, Name: o.StockName, Side: o.Type, Kind: o.Kind,
			Quantity: o.Quantity, Expires: o.ExpiresAt.String, Placed: o.CreatedAt, ViaAI: o.Source != "",
		}
		if o.Kind == "limit" {
			lp := moneyOf(o.LimitPrice, o.Currency)
			row.LimitPrice = &lp
		}
		out = append(out, row)
	}
	return out, nil
}

type apiCancelled struct {
	Cancelled int64  `json:"cancelled"`
	Message   string `json:"message"`
}

func (h *Handler) apiCancel(ctx context.Context, user *db.User, orderID int64) (apiCancelled, error) {
	if !auth.CanTrade(ctx) {
		return apiCancelled{}, errReadOnly
	}
	if _, err := h.CancelOrder(ctx, user.ID, orderID); err != nil {
		return apiCancelled{}, err
	}
	return apiCancelled{Cancelled: orderID, Message: "Order cancelled"}, nil
}

// --- History and leaderboard ---

type apiHistoryRow struct {
	Kind     string `json:"kind"` // buy | sell | interest | dividend | bonus
	Symbol   string `json:"symbol,omitempty"`
	Name     string `json:"name,omitempty"` // the company, or the friend whose sign-up paid a bonus
	Shares   int64  `json:"shares,omitempty"`
	Price    *money `json:"price,omitempty"`
	PerShare string `json:"dividend_per_share,omitempty"` // "$0.176": a dividend can be part of a cent
	Amount   money  `json:"amount"`                       // in the game's currency
	Fee      *money `json:"fee,omitempty"`
	ViaAI    bool   `json:"via_ai,omitempty"`
	At       string `json:"at"` // UTC
}

// apiHistory is the newest limit rows of the player's cash history.
func (h *Handler) apiHistory(ctx context.Context, game db.Game, p db.Participant, limit int) ([]apiHistoryRow, error) {
	rows, err := h.q.ListHistory(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = apiListLimit
	}
	out := []apiHistoryRow{}
	for i := len(rows) - 1; i >= 0 && len(out) < limit; i-- {
		r := rows[i]
		row := apiHistoryRow{Kind: r.Kind, Amount: moneyOf(r.Amount, game.BaseCurrency), ViaAI: r.Source != "", At: r.CreatedAt}
		switch r.Kind {
		case "buy", "sell":
			price := moneyOf(r.Price, r.Currency)
			fee := moneyOf(r.Fee, game.BaseCurrency)
			row.Symbol, row.Name, row.Shares, row.Price, row.Fee = r.Symbol, r.Label, r.Quantity, &price, &fee
		case "dividend":
			row.Symbol, row.Name, row.Shares, row.PerShare = r.Symbol, r.Label, r.Quantity, market.FmtPerShare(r.Price)+" "+r.Currency
		case "bonus":
			row.Name = r.Label
		}
		out = append(out, row)
	}
	return out, nil
}

type apiLeaderboard struct {
	Game    string        `json:"game"`
	Players int           `json:"players"` // players who have traded: only they are ranked
	Ranked  bool          `json:"ranked"`  // whether the player is: false (and you.rank 0) until their first trade
	You     apiStanding   `json:"you"`
	Top     []apiStanding `json:"top"`
}

type apiStanding struct {
	Rank   int    `json:"rank"`
	Name   string `json:"name"`
	Value  money  `json:"value"`
	Return string `json:"return"`
	UsesAI bool   `json:"uses_ai,omitempty"` // has traded through an AI assistant this game
}

func (h *Handler) apiLeaderboard(ctx context.Context, game db.Game, p db.Participant, user *db.User) apiLeaderboard {
	board, _ := h.q.ListParticipantsByGame(ctx, game.ID)
	st := h.standingIn(ctx, p, board)
	standingOf := func(rank int, name string, value int64, ai bool) apiStanding {
		return apiStanding{
			Rank: rank, Name: name, Value: moneyOf(value, game.BaseCurrency),
			Return: change{value - game.StartingBalance, game.StartingBalance}.Pct(), UsesAI: ai,
		}
	}
	out := apiLeaderboard{Game: game.Name, Players: st.Of, Ranked: !st.Unranked, Top: []apiStanding{}}
	rank := 0
	for i, b := range board {
		// Equal values share a place.
		if i == 0 || b.PortfolioValue != board[i-1].PortfolioValue {
			rank = i + 1
		}
		if b.ID == p.ID {
			out.You = standingOf(rank, b.UserName, b.PortfolioValue, b.UsesAi != 0)
		}
		if i < 20 {
			out.Top = append(out.Top, standingOf(rank, b.UserName, b.PortfolioValue, b.UsesAi != 0))
		}
	}
	if out.You.Rank == 0 {
		out.You = standingOf(st.Rank, user.Name, p.PortfolioValue, false) // rank 0 if unranked
	}
	return out
}

// --- HTTP ---

// SetAPILimiter limits each player's API and MCP requests a minute.
func (h *Handler) SetAPILimiter(requests *RateLimiter) {
	h.apiLimiter = requests
}

// APILimit refuses a player's requests past the per-minute limit. It goes
// after auth.RequireToken.
func (h *Handler) APILimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if h.apiLimiter != nil && user != nil && !h.apiLimiter.Allow(strconv.FormatInt(user.ID, 10)) {
			tooMany(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// aiOrderLimit is how many orders the player's AI may place a day.
func aiOrderLimit(user *db.User) int64 {
	if user.AiOrdersPerDay > 0 {
		return user.AiOrdersPerDay
	}
	return AIOrdersPerDay
}

// aiDay is the day AI orders count against: today in TIMEZONE.
func (h *Handler) aiDay() string {
	return time.Now().In(h.loc).Format("2006-01-02")
}

// reserveAIOrder counts an order against the player's AI's daily limit, or
// refuses it once the limit is used up. Call release if the order is then
// refused, so it doesn't count.
func (h *Handler) reserveAIOrder(ctx context.Context, user *db.User) (release func(), err error) {
	day := h.aiDay()
	if err := h.q.StartAIOrderDay(ctx, db.StartAIOrderDayParams{UserID: user.ID, Day: day}); err != nil {
		return nil, err
	}
	limit := aiOrderLimit(user)
	n, err := h.q.ReserveAIOrder(ctx, db.ReserveAIOrderParams{UserID: user.ID, Day: day, MaxOrders: limit})
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, refuse("Your AI has placed its %d orders for today. It can place more from midnight %s, and you can still trade on the site. To ask for a higher limit, write to us at %s/contact.",
			limit, time.Now().In(h.loc).Format("MST"), publicURL())
	}
	return func() {
		_ = h.q.ReleaseAIOrder(context.WithoutCancel(ctx), db.ReleaseAIOrderParams{UserID: user.ID, Day: day})
	}, nil
}

func tooMany(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "60")
	writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "Too many requests. Wait a minute and try again."})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

// apiErrorStatus is the status and message for an error from the API's
// functions. Anything unexpected is logged and not shown.
func apiErrorStatus(err error) (int, string) {
	var refused *OrderError
	switch {
	case errors.As(err, &refused):
		return http.StatusUnprocessableEntity, refused.Msg
	case errors.Is(err, errNotFound):
		return http.StatusNotFound, "Not found"
	case errors.Is(err, errReadOnly):
		return http.StatusForbidden, "This connection can only read. To let your AI trade, make a key that can trade at " + publicURL() + "/settings/api, or reconnect and allow trading."
	}
	slog.Error("api", "error", err)
	return http.StatusInternalServerError, "Something went wrong. Try again in a minute."
}

func writeAPIError(w http.ResponseWriter, err error) {
	status, msg := apiErrorStatus(err)
	writeJSON(w, status, map[string]string{"error": msg})
}

// apiRoute answers a GET for the player with what fn returns.
func (h *Handler) apiRoute(fn func(r *http.Request, user *db.User, game db.Game, p db.Participant) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		game, p, err := h.apiPlayer(r.Context(), user.ID)
		if err != nil {
			writeAPIError(w, err)
			return
		}
		v, err := fn(r, user, game, p)
		if err != nil {
			writeAPIError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, v)
	}
}

// queryInt reads a whole number from the query string, or 0.
func queryInt(r *http.Request, name string) int {
	n, _ := strconv.Atoi(r.URL.Query().Get(name))
	return n
}

// APIRoutes are /api/v1's routes, behind auth.RequireToken and APILimit.
func (h *Handler) APIRoutes(r chi.Router) {
	r.Get("/me", h.APIMe)
	r.Get("/game", h.apiRoute(func(r *http.Request, user *db.User, g db.Game, _ db.Participant) (any, error) {
		return h.apiGameInfo(r.Context(), user, g), nil
	}))
	r.Get("/portfolio", h.apiRoute(func(r *http.Request, _ *db.User, g db.Game, p db.Participant) (any, error) {
		return h.apiPortfolio(r.Context(), g, p), nil
	}))
	r.Get("/stocks", h.apiRoute(func(r *http.Request, _ *db.User, g db.Game, p db.Participant) (any, error) {
		v := r.URL.Query()
		return h.apiStocks(r.Context(), g, p, v.Get("filter"), v.Get("sector"), v.Get("q"), queryInt(r, "limit"))
	}))
	r.Get("/stocks/{symbol}", h.apiRoute(func(r *http.Request, _ *db.User, g db.Game, p db.Participant) (any, error) {
		return h.apiStock(r.Context(), g, p, r.PathValue("symbol"))
	}))
	r.Get("/orders", h.apiRoute(func(r *http.Request, _ *db.User, _ db.Game, p db.Participant) (any, error) {
		return h.apiOrders(r.Context(), p)
	}))
	r.Get("/history", h.apiRoute(func(r *http.Request, _ *db.User, g db.Game, p db.Participant) (any, error) {
		return h.apiHistory(r.Context(), g, p, queryInt(r, "limit"))
	}))
	r.Get("/leaderboard", h.apiRoute(func(r *http.Request, u *db.User, g db.Game, p db.Participant) (any, error) {
		return h.apiLeaderboard(r.Context(), g, p, u), nil
	}))
	r.Post("/orders/preview", h.APIOrderPreview)
	r.Post("/orders", h.APIOrderCreate)
	r.Delete("/orders/{oid}", h.APIOrderCancel)
}

type apiMe struct {
	Name     string  `json:"name"`
	Access   string  `json:"access"` // read, or trade
	Via      string  `json:"via"`    // the key's or app's name
	Game     apiGame `json:"game"`
	Settings string  `json:"settings"`
}

// APIMe says who the token belongs to and what it may do.
func (h *Handler) APIMe(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	game, _, err := h.apiPlayer(r.Context(), user.ID)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h.apiMe(r.Context(), user, game))
}

func (h *Handler) apiMe(ctx context.Context, user *db.User, game db.Game) apiMe {
	tok := auth.TokenFromContext(ctx)
	return apiMe{
		Name:     user.Name,
		Access:   tok.Scope,
		Via:      tok.Name,
		Game:     h.apiGameInfo(ctx, user, game),
		Settings: publicURL() + "/settings/api",
	}
}

// readOrder decodes an order from a JSON body.
func readOrder(r *http.Request) (apiOrderRequest, error) {
	var req apiOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return req, refuse("Send the order as JSON: {\"symbol\": \"NVDA\", \"side\": \"buy\", \"quantity\": 10}")
	}
	return req, nil
}

// APIOrderPreview works out an order without placing it.
func (h *Handler) APIOrderPreview(w http.ResponseWriter, r *http.Request) {
	req, err := readOrder(r)
	if err == nil {
		var res apiOrderResult
		if res, err = h.apiOrder(r.Context(), auth.UserFromContext(r.Context()), req, "api", true); err == nil {
			writeJSON(w, http.StatusOK, res)
			return
		}
	}
	writeAPIError(w, err)
}

// maxIdempotencyKey is the longest Idempotency-Key kept.
const maxIdempotencyKey = 200

// APIOrderCreate places an order. With an Idempotency-Key header, a retry
// with the same key after an order went through gets the first answer again
// instead of a second order.
func (h *Handler) APIOrderCreate(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(key) > maxIdempotencyKey {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Idempotency-Key is too long"})
		return
	}
	if key != "" {
		n, err := h.q.ClaimIdempotencyKey(r.Context(), db.ClaimIdempotencyKeyParams{UserID: user.ID, Key: key})
		if err != nil {
			writeAPIError(w, err)
			return
		}
		if n == 0 {
			prev, err := h.q.GetIdempotency(r.Context(), db.GetIdempotencyParams{UserID: user.ID, Key: key})
			if err != nil || prev.Status == 0 {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "An order with this Idempotency-Key is still being placed. Check GET /api/v1/orders and /history before trying again."})
				return
			}
			w.Header().Set("Idempotent-Replayed", "true")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(int(prev.Status))
			w.Write([]byte(prev.Response))
			return
		}
	}

	status, body := h.orderResponse(r, user)
	if key != "" {
		if status >= 300 {
			// Refused or failed: nothing was placed, so a retry with the
			// same key may try again (after a minute, or once there's cash).
			_ = h.q.ReleaseIdempotencyKey(r.Context(), db.ReleaseIdempotencyKeyParams{UserID: user.ID, Key: key})
		} else {
			_ = h.q.SaveIdempotency(r.Context(), db.SaveIdempotencyParams{Status: int64(status), Response: string(body), UserID: user.ID, Key: key})
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	w.Write(body)
}

// orderResponse places the order in r and returns the status and JSON body
// to answer with.
func (h *Handler) orderResponse(r *http.Request, user *db.User) (int, []byte) {
	var status int
	var v any
	if req, err := readOrder(r); err != nil {
		status, v = apiErrorBody(err)
	} else if res, err := h.apiOrder(r.Context(), user, req, "api", false); err != nil {
		status, v = apiErrorBody(err)
	} else {
		status, v = http.StatusCreated, res
	}
	body, _ := json.Marshal(v)
	return status, append(body, '\n')
}

func apiErrorBody(err error) (int, any) {
	status, msg := apiErrorStatus(err)
	return status, map[string]string{"error": msg}
}

// APIOrderCancel cancels one of the player's open orders.
func (h *Handler) APIOrderCancel(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("oid"), 10, 64)
	if err != nil {
		writeAPIError(w, errNotFound)
		return
	}
	res, err := h.apiCancel(r.Context(), auth.UserFromContext(r.Context()), id)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
