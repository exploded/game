package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The MCP server at /mcp gives a player's own AI the API's functions as
// tools: Claude and ChatGPT add it as a connector (signing in through
// oauth.go), and agents such as Claude Code connect with an API key. It's
// stateless, answering each POST with JSON, so nothing streams through the
// compression middleware or Caddy.

const mcpInstructions = `The Wealth Lab is a monthly share game with play money: each player starts the month with $1,000,000 to invest in US-listed stocks, and the biggest portfolio at the end of the month wins. These tools act for one player in this month's game.

- Before place_order, call preview_order and show the player what it will cost, then place it only once they agree. Skip the confirmation only when the player has told you to trade without asking.
- Find symbols with find_stocks. Amounts are given in cents with formatted text beside them; quantities are whole shares.
- Prices are delayed. While the US market is closed, a market order either fills at the last close or waits for the open, as get_game says.
- The player's AI can place a set number of orders a day (get_game gives ai_orders_left_today). Previews, cancels and refused orders don't count. Once none are left, tell the player rather than retrying.
- This is a game, and nothing here is financial advice. Don't present your own suggestions as advice from The Wealth Lab.`

// mcpSchemas keeps the tools' JSON schemas, which a new server per request
// would otherwise work out every time.
var mcpSchemas = mcp.NewSchemaCache()

// MCPHandler serves /mcp. It goes behind auth.RequireToken and APILimit.
func (h *Handler) MCPHandler() http.Handler {
	return mcp.NewStreamableHTTPHandler(h.mcpServer, &mcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
		// Caddy proxies from 127.0.0.1 with the site's Host header, which the
		// localhost check would refuse. Every request needs a token anyway.
		DisableLocalhostProtection: true,
	})
}

type mcpNoInput struct{}

type mcpFindStocks struct {
	Query  string `json:"query,omitempty" jsonschema:"Search by symbol or company name, such as NVDA or nvidia. Searches every stock and ignores filter and sector."`
	Filter string `json:"filter,omitempty" jsonschema:"A list or financial filter from the filters this tool returns, such as picks (Rob's Picks, the default), all, mag7, tech, up (gainers), down (losers), big, active (most traded), aus, intl, crypto, popular (players' favourites) or score (health score 4 or 5)."`
	Sector string `json:"sector,omitempty" jsonschema:"Only stocks in this sector, such as Information Technology."`
	Limit  int    `json:"limit,omitempty" jsonschema:"How many stocks to return. Default 50."`
}

type mcpSymbol struct {
	Symbol string `json:"symbol" jsonschema:"The stock's symbol, such as NVDA."`
}

type mcpOrder struct {
	Symbol     string  `json:"symbol" jsonschema:"The stock's symbol, such as NVDA."`
	Side       string  `json:"side" jsonschema:"buy or sell. Selling more than you hold goes short, if the game allows it."`
	Quantity   int64   `json:"quantity,omitempty" jsonschema:"Whole shares. Leave out with close_all."`
	Type       string  `json:"type,omitempty" jsonschema:"market (the default) or limit."`
	LimitPrice float64 `json:"limit_price,omitempty" jsonschema:"For a limit order: dollars a share in the stock's currency. A buy fills at or below it, a sell at or above."`
	Expires    string  `json:"expires,omitempty" jsonschema:"For a limit order: the last day it stays open, YYYY-MM-DD. Open orders are cancelled when the game ends."`
	CloseAll   bool    `json:"close_all,omitempty" jsonschema:"Close the whole position: sell every share held, or buy back a whole short. Use with side sell for shares held, buy for a short."`
}

func (o mcpOrder) request() apiOrderRequest {
	return apiOrderRequest{
		Symbol: o.Symbol, Side: o.Side, Quantity: o.Quantity, Type: o.Type,
		LimitPrice: o.LimitPrice, Expires: o.Expires, CloseAll: o.CloseAll,
	}
}

type mcpOrderID struct {
	OrderID int64 `json:"order_id" jsonschema:"The order's id, from list_orders."`
}

type mcpLimit struct {
	Limit int `json:"limit,omitempty" jsonschema:"How many entries to return, newest first. Default 50."`
}

type mcpOrders struct {
	Orders []apiOrderRow `json:"orders"`
}

type mcpHistory struct {
	History []apiHistoryRow `json:"history"`
}

// mcpServer is the server for one request, its tools acting as the request's
// player.
func (h *Handler) mcpServer(r *http.Request) *mcp.Server {
	user := auth.UserFromContext(r.Context())
	tok := auth.TokenFromContext(r.Context())
	s := mcp.NewServer(&mcp.Implementation{
		Name:       "the-wealth-lab",
		Title:      "The Wealth Lab",
		Version:    "1.0.0",
		WebsiteURL: publicURL(),
	}, &mcp.ServerOptions{Instructions: mcpInstructions, SchemaCache: mcpSchemas})

	// asPlayer gives a tool's context the request's player and token.
	asPlayer := func(ctx context.Context) context.Context { return auth.WithToken(ctx, user, tok) }
	// inGame runs fn with this month's game and the player's place in it.
	inGame := func(ctx context.Context, fn func(ctx context.Context, g db.Game, p db.Participant) error) error {
		ctx = asPlayer(ctx)
		g, p, err := h.apiPlayer(ctx, user.ID)
		if err != nil {
			return err
		}
		return fn(ctx, g, p)
	}
	read := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: new(false)}

	addTool(s, &mcp.Tool{
		Name:        "get_game",
		Description: "This month's game: dates, fees, rules, whether the US market is open, and how many orders the player's AI has left today.",
		Annotations: read,
	}, func(ctx context.Context, _ mcpNoInput) (out apiGame, err error) {
		err = inGame(ctx, func(ctx context.Context, g db.Game, p db.Participant) error {
			out = h.apiGameInfo(ctx, user, g)
			return nil
		})
		return out, err
	})
	addTool(s, &mcp.Tool{
		Name:        "get_portfolio",
		Description: "The player's cash, total value, today's move and since the start, leaderboard rank, and each holding.",
		Annotations: read,
	}, func(ctx context.Context, _ mcpNoInput) (out apiPortfolio, err error) {
		err = inGame(ctx, func(ctx context.Context, g db.Game, p db.Participant) error {
			out = h.apiPortfolio(ctx, g, p)
			return nil
		})
		return out, err
	})
	addTool(s, &mcp.Tool{
		Name:        "find_stocks",
		Description: "Search for stocks or list them by a filter, with price, today's move, health score and Rob's Picks. Returns the filters and sectors it knows.",
		Annotations: read,
	}, func(ctx context.Context, in mcpFindStocks) (out apiStockList, err error) {
		err = inGame(ctx, func(ctx context.Context, g db.Game, p db.Participant) error {
			out, err = h.apiStocks(ctx, g, p, in.Filter, in.Sector, in.Query, in.Limit)
			return err
		})
		return out, err
	})
	addTool(s, &mcp.Tool{
		Name:        "get_stock",
		Description: "One stock: latest price and change, 30 days of closes, financial figures, what the company does, and the player's position in it.",
		Annotations: read,
	}, func(ctx context.Context, in mcpSymbol) (out apiStock, err error) {
		err = inGame(ctx, func(ctx context.Context, g db.Game, p db.Participant) error {
			out, err = h.apiStock(ctx, g, p, in.Symbol)
			return err
		})
		return out, err
	})
	addTool(s, &mcp.Tool{
		Name:        "preview_order",
		Description: "Work out an order without placing it: price, fees, total and the cash left, and whether it would fill now, wait for the open, or wait for its limit price. Show this to the player before place_order.",
		Annotations: read,
	}, func(ctx context.Context, in mcpOrder) (apiOrderResult, error) {
		return h.apiOrder(asPlayer(ctx), user, in.request(), "mcp", true)
	})
	addTool(s, &mcp.Tool{
		Name:        "place_order",
		Description: "Place a buy or sell order for the player. Unless the player has told you to trade without asking, call preview_order first and get their agreement.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true), OpenWorldHint: new(false)},
	}, func(ctx context.Context, in mcpOrder) (apiOrderResult, error) {
		return h.apiOrder(asPlayer(ctx), user, in.request(), "mcp", false)
	})
	addTool(s, &mcp.Tool{
		Name:        "list_orders",
		Description: "The player's open orders: limit orders waiting for their price, and market orders waiting for the US market to open.",
		Annotations: read,
	}, func(ctx context.Context, _ mcpNoInput) (out mcpOrders, err error) {
		err = inGame(ctx, func(ctx context.Context, g db.Game, p db.Participant) error {
			out.Orders, err = h.apiOrders(ctx, p)
			return err
		})
		return out, err
	})
	addTool(s, &mcp.Tool{
		Name:        "cancel_order",
		Description: "Cancel one of the player's open orders.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true), IdempotentHint: true, OpenWorldHint: new(false)},
	}, func(ctx context.Context, in mcpOrderID) (apiCancelled, error) {
		return h.apiCancel(asPlayer(ctx), user, in.OrderID)
	})
	addTool(s, &mcp.Tool{
		Name:        "get_history",
		Description: "The player's trades, interest, dividends and bonuses in this month's game, newest first.",
		Annotations: read,
	}, func(ctx context.Context, in mcpLimit) (out mcpHistory, err error) {
		err = inGame(ctx, func(ctx context.Context, g db.Game, p db.Participant) error {
			out.History, err = h.apiHistory(ctx, g, p, in.Limit)
			return err
		})
		return out, err
	})
	addTool(s, &mcp.Tool{
		Name:        "get_leaderboard",
		Description: "The top 20 players in this month's game and where the player stands.",
		Annotations: read,
	}, func(ctx context.Context, _ mcpNoInput) (out apiLeaderboard, err error) {
		err = inGame(ctx, func(ctx context.Context, g db.Game, p db.Participant) error {
			out = h.apiLeaderboard(ctx, g, p, user)
			return nil
		})
		return out, err
	})
	return s
}

// addTool adds a tool whose errors reach the model as the message a player
// would see, never an internal error's text.
func addTool[In, Out any](s *mcp.Server, t *mcp.Tool, fn func(context.Context, In) (Out, error)) {
	mcp.AddTool(s, t, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		out, err := fn(ctx, in)
		if err != nil {
			_, msg := apiErrorStatus(err)
			var zero Out
			return nil, zero, errors.New(msg)
		}
		return nil, out, nil
	})
}
